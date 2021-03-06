/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/Shopify/sarama"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
)

const (
	ipv4Len = 4
	ipv6Len = 16

	currKafkaVersion = `2.1.1`
)

type closer interface {
	Close() error
}

type closers struct {
	mtx sync.Mutex
	wg  sync.WaitGroup
	set []closer
}

func newClosers() *closers {
	return &closers{}
}

func (c *closers) add(v closer) (wg *sync.WaitGroup) {
	if v == nil {
		return nil
	}
	c.mtx.Lock()
	c.set = append(c.set, v)
	wg = &c.wg
	c.mtx.Unlock()
	return
}

func (c *closers) Close() (err error) {
	c.mtx.Lock()
	for _, v := range c.set {
		err = appendError(err, v.Close())
	}
	c.wg.Wait()
	c.set = nil
	c.mtx.Unlock()
	return
}

func appendError(base, next error) error {
	if next == nil {
		return base
	} else if base == nil {
		return next
	}
	return fmt.Errorf("%v %v", base, next)
}

type kafkaConsumer struct {
	kafkaConsumerConfig
	mtx      sync.Mutex
	started  bool
	tag      entry.EntryTag
	ctx      context.Context
	cf       context.CancelFunc
	count    uint
	size     uint
	memberId string
	src      net.IP
}

type kafkaConsumerConfig struct {
	consumerCfg
	igst  *ingest.IngestMuxer
	lg    *log.Logger
	pproc *processors.ProcessorSet
}

func newKafkaConsumer(cfg kafkaConsumerConfig) (kc *kafkaConsumer, err error) {
	if cfg.igst == nil {
		err = errors.New("nil ingest connection")
	} else if cfg.lg == nil {
		err = errors.New("nil logger")
	} else {
		kc = &kafkaConsumer{
			kafkaConsumerConfig: cfg,
		}
		if kc.tag, err = cfg.igst.GetTag(cfg.tag); err != nil {
			kc = nil
		}
		kc.ctx, kc.cf = context.WithCancel(context.Background())
	}
	return
}

func (kc *kafkaConsumer) Start(wg *sync.WaitGroup) (err error) {
	kc.mtx.Lock()
	if kc.started {
		err = errors.New("already started")
	} else if kc.ctx == nil || kc.cf == nil {
		err = errors.New("closer context is nil, already closed")
	} else {
		cfg := sarama.NewConfig()
		if cfg.Version, err = sarama.ParseKafkaVersion(currKafkaVersion); err != nil {
			return
		}
		cfg.Consumer.Group.Rebalance.Strategy = kc.strat
		cfg.Consumer.Offsets.Initial = sarama.OffsetOldest
		var clnt sarama.ConsumerGroup
		if clnt, err = sarama.NewConsumerGroup([]string{kc.leader}, kc.group, cfg); err != nil {
			return
		}
		wg.Add(1)
		kc.started = true
		go kc.routine(clnt, wg)
	}
	kc.mtx.Unlock()
	return
}

//close the connection
func (kc *kafkaConsumer) Close() (err error) {
	if kc == nil {
		err = errors.New("nil consumer")
	} else {
		kc.mtx.Lock()
		if kc.cf == nil {
			err = errors.New("nil closer conn, routine closed")
		} else {
			kc.cf()
			kc.cf = nil
		}
		kc.mtx.Unlock()
	}
	return
}

func (kc *kafkaConsumer) routine(client sarama.ConsumerGroup, wg *sync.WaitGroup) {
	defer wg.Done()
	var i int
	for {
		i++
		kc.lg.Info("Consumer start attempt %d\n", i)
		if err := client.Consume(kc.ctx, []string{kc.topic}, kc); err != nil {
			kc.lg.Error("Consumer error: %v", err)
			break
		}
		if kc.ctx.Err() != nil {
			break
		}
	}
}

//Setup can handle setup and gets a chance to fire up internal state prior to starting
func (kc *kafkaConsumer) Setup(cgs sarama.ConsumerGroupSession) (err error) {
	kc.lg.Info("Kafka consumer %s starting\n", cgs.MemberID())
	//update our member id and reset the count
	//also get a local handle on the ingest muxer and wait for a hot connection
	kc.mtx.Lock()
	kc.memberId = cgs.MemberID()
	kc.count = 0
	kc.size = 0
	igst := kc.igst
	kc.mtx.Unlock()
	kc.lg.Info("Kafka consumer %s waiting for hot ingester\n", cgs.MemberID())
	if err = igst.WaitForHotContext(kc.ctx, 0); err == nil {
		kc.lg.Info("Kafka consumer %s getting source ip\n", cgs.MemberID())
		kc.src, err = igst.SourceIP()
	}
	kc.lg.Info("Consumer setup complete, got source %s\n", kc.src)
	return
}

//Cleanup executes at the end of a session, this a chance to clean up and sync our ingester
func (kc *kafkaConsumer) Cleanup(cgs sarama.ConsumerGroupSession) (err error) {
	kc.lg.Info("Kafka consumer %s cleaning up\n", cgs.MemberID())
	//get a local handle on the ingest muxer
	kc.mtx.Lock()
	igst := kc.igst
	kc.mtx.Unlock()

	if igst != nil {
		igst.Info("Kafka group %s (%s) wrote %d entries %d bytes",
			kc.group, kc.memberId, kc.count, kc.size)
		if err = igst.Sync(0); err != nil {
			kc.lg.Info("Consumer cleanup failed: %v\n", err)
		} else {
			kc.lg.Info("Consumer cleanup complete\n")
		}
	}
	return
}

//ConsumeClaim actually eats entries from the session and writes them into our ingester
func (kc *kafkaConsumer) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	//README the ConsumeClaim function is running in a go routine
	//it is entirely possible for multiple of these routines to be running at a time

	if claim.Topic() != kc.topic {
		return errors.New("Claim routine got the wrong topic")
	}

	tckr := time.NewTicker(time.Second)
	defer tckr.Stop()
	rch := claim.Messages()

	var currTS int64
	batch := make([]*sarama.ConsumerMessage, 0, kc.batchSize)

	kc.lg.Info("Consumer %s group %s started\n", kc.memberId, kc.group)
loop:
	for {
		select {
		case msg, ok := <-rch:
			if !ok {
				break loop
			} else if msg == nil {
				continue
			}
			ts := msg.Timestamp.Unix()
			if currTS != ts && len(batch) > 0 {
				//flush the existing batch
				if err := kc.flush(session, batch); err != nil {
					kc.lg.Error("Failed to write %d entries: %v", len(batch), err)
					return err
				}
				batch = batch[0:0]
			}
			batch = append(batch, msg)
			currTS = ts
			//check if we hit capacity
			if len(batch) == cap(batch) {
				//flush the existing batch
				if err := kc.flush(session, batch); err != nil {
					kc.lg.Error("Failed to write %d entries: %v", len(batch), err)
					return err
				}
				currTS = 0
				batch = batch[0:0]
			}
		case <-tckr.C:
			if len(batch) > 0 {
				//flush the existing batch
				if err := kc.flush(session, batch); err != nil {
					kc.lg.Error("Failed to write %d entries: %v", len(batch), err)
					return err
				}
				currTS = 0
				batch = batch[0:0]
			}
		}
	}
	kc.lg.Info("Consumer %s group %s exited\n", kc.memberId, kc.group)
	return nil
}

func (kc *kafkaConsumer) flush(session sarama.ConsumerGroupSession, msgs []*sarama.ConsumerMessage) (err error) {
	var sz uint
	var cnt uint
	for _, m := range msgs {
		ent := &entry.Entry{
			Tag:  kc.tag,
			TS:   entry.FromStandard(m.Timestamp),
			Data: m.Value,
			SRC:  kc.extractSource(m),
		}
		if kc.ignoreTS {
			ent.TS = entry.Now()
		} else if kc.extractTS && kc.tg != nil {
			var hts time.Time
			var ok bool
			if hts, ok, err = kc.tg.Extract(ent.Data); err != nil {
				kc.lg.Warn("Catastrophic error from timegrinder: %v", err)
			} else if ok {
				ent.TS = entry.FromStandard(hts)
			}
			// if not ok, we'll just use the timestamp
		}
		if err = kc.pproc.ProcessContext(ent, kc.ctx); err != nil {
			return
		}
		sz += uint(ent.Size())
		cnt++
	}
	if kc.sync {
		if err = kc.igst.SyncContext(kc.ctx, time.Second); err != nil {
			return
		}
	}
	//commit the messages
	for i := range msgs {
		session.MarkMessage(msgs[i], ``)
	}
	kc.count += cnt
	kc.size += sz
	return
}

func (kc *kafkaConsumer) extractSource(m *sarama.ConsumerMessage) (ip net.IP) {
	//short circuit out
	if m == nil {
		ip = kc.src
		return
	}
	if kc.headerKeyAsSrc != nil {
		for _, rh := range m.Headers {
			if bytes.Equal(kc.headerKeyAsSrc, rh.Key) {
				ip = kc.extractSrc(rh.Value)
			}
		}
	}
	//if we didn't get anything, try again with the key as source (if set)
	if ip == nil && kc.keyAsSrc {
		ip = kc.extractSrc(m.Key)
	}
	//if if we still missed, just use the src
	if ip == nil {
		ip = kc.src
	}
	return
}

func (kc *kafkaConsumer) extractSrc(v []byte) (ip net.IP) {
	if kc.srcAsText {
		ip = net.ParseIP(string(v))
	} else if len(v) == ipv4Len || len(v) == ipv6Len {
		ip = net.IP(v)
	}
	return
}
