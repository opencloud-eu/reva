// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package goroutinelock

import (
	"bytes"
	"runtime"
	"strconv"
	"sync/atomic"
)

// Lock tracks which goroutine currently owns a metadata lock.
//
// Held() only returns true in the owning goroutine, which prevents lock-held
// state from leaking across goroutine boundaries when helper nodes are shared.
type Lock struct {
	gid atomic.Uint64
}

func (o *Lock) Hold() {
	o.gid.Store(goid())
}

func (o *Lock) Release() {
	o.gid.Store(0)
}

func (o *Lock) Held() bool {
	return o.gid.Load() == goid()
}

func goid() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)

	// The header has the form "goroutine 1234 [running]:".
	fields := bytes.Fields(buf[:n])
	if len(fields) < 2 {
		return 0
	}
	id, err := strconv.ParseUint(string(fields[1]), 10, 64)
	if err != nil {
		return 0
	}
	return id
}
