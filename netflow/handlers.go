/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/calmh/ipfix"
	"github.com/gravwell/ingest/entry"
	"github.com/gravwell/netflow"
)

var (
	ErrAlreadyListening = errors.New("Already listening")
	ErrAlreadyClosed    = errors.New("Already closed")
	ErrNotReady         = errors.New("Not Ready")
)

type NetflowV5Handler struct {
	bindConfig
	mtx   *sync.Mutex
	c     *net.UDPConn
	ready bool
}

func NewNetflowV5Handler(c bindConfig) (*NetflowV5Handler, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	return &NetflowV5Handler{
		bindConfig: c,
		mtx:        &sync.Mutex{},
	}, nil
}

func (n *NetflowV5Handler) String() string {
	return `NetflowV5`
}

func (n *NetflowV5Handler) Listen(s string) (err error) {
	n.mtx.Lock()
	defer n.mtx.Unlock()
	if n.c != nil {
		err = ErrAlreadyListening
		return
	}
	var a *net.UDPAddr
	if a, err = net.ResolveUDPAddr("udp", s); err != nil {
		return
	}
	if n.c, err = net.ListenUDP("udp", a); err == nil {
		n.ready = true
	}
	return
}

func (n *NetflowV5Handler) Close() error {
	n.mtx.Lock()
	defer n.mtx.Unlock()
	if n == nil {
		return ErrAlreadyClosed
	}
	n.ready = false
	return n.c.Close()
}

func (n *NetflowV5Handler) Start(id int) error {
	n.mtx.Lock()
	defer n.mtx.Unlock()
	if !n.ready || n.c == nil {
		fmt.Println(n.ready, n.c)
		return ErrNotReady
	}
	if id < 0 {
		return errors.New("invalid id")
	}
	go n.routine(id)
	return nil
}

func (n *NetflowV5Handler) routine(id int) {
	defer n.wg.Done()
	defer delConn(id)
	var nf netflow.NFv5
	var l int
	var addr *net.UDPAddr
	var err error
	var ts entry.Timestamp
	tbuff := make([]byte, netflow.HeaderSize+(30*netflow.RecordSize))
	for {
		if l, addr, err = n.c.ReadFromUDP(tbuff); err != nil {
			return
		}
		if l, err = nf.ValidateSize(tbuff); err != nil {
			continue //there isn't much we can do about bad packets...
		}
		lbuff := make([]byte, l)
		copy(lbuff, tbuff[0:l])
		if n.ignoreTS {
			ts = entry.Now()
		} else {
			ts = entry.UnixTime(int64(binary.BigEndian.Uint32(lbuff[8:12])), int64(binary.BigEndian.Uint32(lbuff[12:16])))
		}
		e := &entry.Entry{
			Tag:  n.tag,
			SRC:  addr.IP,
			TS:   ts,
			Data: lbuff,
		}
		n.ch <- e
	}
}

type IpfixHandler struct {
	bindConfig
	mtx   *sync.Mutex
	c     *net.UDPConn
	ready bool
}

func NewIpfixHandler(c bindConfig) (*IpfixHandler, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	return &IpfixHandler{
		bindConfig: c,
		mtx:        &sync.Mutex{},
	}, nil
}

func (i *IpfixHandler) String() string {
	return `Ipfix`
}

func (i *IpfixHandler) Listen(s string) (err error) {
	i.mtx.Lock()
	defer i.mtx.Unlock()
	if i.c != nil {
		err = ErrAlreadyListening
		return
	}
	var a *net.UDPAddr
	if a, err = net.ResolveUDPAddr("udp", s); err != nil {
		return
	}
	if i.c, err = net.ListenUDP("udp", a); err == nil {
		i.ready = true
	}
	return
}

func (i *IpfixHandler) Close() error {
	i.mtx.Lock()
	defer i.mtx.Unlock()
	if i == nil {
		return ErrAlreadyClosed
	}
	i.ready = false
	return i.c.Close()
}

func (i *IpfixHandler) Start(id int) error {
	i.mtx.Lock()
	defer i.mtx.Unlock()
	if !i.ready || i.c == nil {
		fmt.Println(i.ready, i.c)
		return ErrNotReady
	}
	if id < 0 {
		return errors.New("invalid id")
	}
	go i.routine(id)
	return nil
}

func (i *IpfixHandler) routine(id int) {
	defer i.wg.Done()
	defer delConn(id)
	var l int
	s := ipfix.NewSession()
	var addr *net.UDPAddr
	var err error
	var ts entry.Timestamp
	tbuff := make([]byte, 65507) // just go with max UDP packet size
	for {
		if l, addr, err = i.c.ReadFromUDP(tbuff); err != nil {
			fmt.Printf("Error in ReadFromUDP: %v\n", err)
			return
		}
		msg, err := s.ParseBuffer(tbuff[:l])
		if err != nil {
			// must have been a bad packet
			continue
		}
		lbuff := make([]byte, l)
		copy(lbuff, tbuff[0:l])
		if i.ignoreTS {
			ts = entry.Now()
		} else {
			ts = entry.UnixTime(int64(msg.Header.ExportTime), 0)
		}
		e := &entry.Entry{
			Tag:  i.tag,
			SRC:  addr.IP,
			TS:   ts,
			Data: lbuff,
		}
		i.ch <- e
	}
}
