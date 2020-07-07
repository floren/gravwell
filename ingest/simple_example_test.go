/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest_test

import (
	"log"
	"net"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

var (
	dst          = "tcp://127.0.0.1:4023"
	sharedSecret = "IngestSecrets"
	simple_tags  = []string{"testtag"}
)

// SimplestExample is the simplest possible example of ingesting a single Entry.
func Example_simplest() {
	// Get an IngestConnection
	ingestConfig := ingest.UniformMuxerConfig{
		Destinations: []string{dst},
		Tags:         simple_tags,
		Auth:         sharedSecret,
		PublicKey:    ``,
		PrivateKey:   ``,
		LogLevel:     "WARN",
	}
	// Start the ingester
	igst, err := ingest.NewUniformMuxer(ingestConfig)
	if err != nil {
		log.Fatalf("Failed build our ingest system: %v\n", err)
	}
	defer igst.Close()
	if err := igst.Start(); err != nil {
		log.Fatalf("Failed start our ingest system: %v\n", err)
	}

	// Wait for connection to indexers
	if err := igst.WaitForHot(0); err != nil {
		log.Fatalf("Timedout waiting for backend connections: %v\n", err)
	}

	// We need to get the numeric value for the tag we're using
	tagid, err := igst.GetTag(simple_tags[0])
	if err != nil {
		log.Fatalf("Failed to get tag: %v", err)
	}

	// Now we'll create an Entry
	ent := entry.Entry{
		TS:   entry.Now(),
		SRC:  net.ParseIP("127.0.0.1"),
		Tag:  tagid,
		Data: []byte("This is my test data!"),
	}

	// And finally write the Entry
	igst.WriteEntry(&ent)
}
