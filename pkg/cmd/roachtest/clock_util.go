// Copyright 2018 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package main

import (
	"context"
	gosql "database/sql"
	"fmt"
	"time"
)

// isAlive returns whether the node queried by db is alive.
func isAlive(db *gosql.DB, l *logger) bool {
	// The cluster might have just restarted, in which case the first call to db
	// might return an error. In fact, the first db.Ping() reliably returns an
	// error (but a db.Exec() only seldom returns an error). So, we're gonna
	// Ping() twice to allow connections to be re-established.
	_ = db.Ping()
	if err := db.Ping(); err != nil {
		l.Printf("isAlive returned err=%v (%T)", err, err)
	} else {
		return true
	}
	return false
}

// dbUnixEpoch returns the current time in db.
func dbUnixEpoch(db *gosql.DB) (float64, error) {
	var epoch float64
	if err := db.QueryRow("SELECT now()::DECIMAL").Scan(&epoch); err != nil {
		return 0, err
	}
	return epoch, nil
}

// offsetInjector is used to inject clock offsets in roachtests.
type offsetInjector struct {
	c        *cluster
	deployed bool
}

// deploy installs ntp and downloads / compiles bumptime used to create a clock offset.
func (oi *offsetInjector) deploy(ctx context.Context) error {
	if err := oi.c.RunE(ctx, oi.c.All(), "test -x ./bumptime"); err == nil {
		oi.deployed = true
		return nil
	}

	if err := oi.c.Install(ctx, oi.c.l, oi.c.All(), "ntp"); err != nil {
		return err
	}
	if err := oi.c.Install(ctx, oi.c.l, oi.c.All(), "gcc"); err != nil {
		return err
	}
	if err := oi.c.RunL(ctx, oi.c.l, oi.c.All(), "sudo", "service", "ntp", "stop"); err != nil {
		return err
	}
	if err := oi.c.RunL(ctx, oi.c.l,
		oi.c.All(),
		"curl",
		"--retry", "3",
		"--fail",
		"--show-error",
		"-kO",
		"https://raw.githubusercontent.com/cockroachdb/jepsen/master/cockroachdb/resources/bumptime.c",
	); err != nil {
		return err
	}
	// bumptime.c calls settimeofday() with a 'struct timezone tz*' argument
	// that causes the executable to fail with an "invalid argument" error
	// on more recent Ubuntus. Quoth `man settimeofday`:
	//     "The use of the timezone structure is obsolete; the tz argument
	//      should normally be specified as NULL."
	if err := oi.c.RunL(ctx, oi.c.l,
		oi.c.All(), "sed", "-i", "s/&tz/NULL/g", "bumptime.c"); err != nil {
		return err
	}
	if err := oi.c.RunL(ctx, oi.c.l,
		oi.c.All(), "gcc", "bumptime.c", "-o", "bumptime", "&&", "rm bumptime.c",
	); err != nil {
		return err
	}
	oi.deployed = true
	return nil
}

// offset injects a offset of s into the node with the given nodeID.
func (oi *offsetInjector) offset(ctx context.Context, nodeID int, s time.Duration) {
	if !oi.deployed {
		oi.c.t.Fatal("Offset injector must be deployed before injecting a clock offset")
	}

	oi.c.Run(
		ctx,
		oi.c.Node(nodeID),
		fmt.Sprintf("sudo ./bumptime %f", float64(s)/float64(time.Millisecond)),
	)
}

// recover force syncs time on the node with the given nodeID to recover
// from any offsets.
func (oi *offsetInjector) recover(ctx context.Context, nodeID int) {
	if !oi.deployed {
		oi.c.t.Fatal("Offset injector must be deployed before recovering from clock offsets")
	}

	syncCmds := [][]string{
		{"sudo", "service", "ntp", "stop"},
		{"sudo", "ntpdate", "-u", "time.google.com"},
		{"sudo", "service", "ntp", "start"},
	}
	for _, cmd := range syncCmds {
		oi.c.Run(
			ctx,
			oi.c.Node(nodeID),
			cmd...,
		)
	}
}

// newOffsetInjector creates a offsetInjector which can be used to inject
// and recover from clock offsets.
func newOffsetInjector(c *cluster) *offsetInjector {
	return &offsetInjector{c: c}
}
