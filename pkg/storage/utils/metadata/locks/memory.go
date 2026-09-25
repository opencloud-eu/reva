// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package locks

import (
	"context"
	"sync"
)

// MemoryLocker is an in-process Locker backed by a map of mutexes, one per
// key. It only serializes within a single process/replica and is suitable for
// single-replica deployments or tests.
type MemoryLocker struct {
	mu sync.Map // key → *sync.Mutex
}

// NewMemoryLocker returns a ready-to-use MemoryLocker.
func NewMemoryLocker() *MemoryLocker {
	return &MemoryLocker{}
}

// Lock acquires the per-key mutex, blocking until it is available. The returned
// function releases the lock exactly once. The context is accepted for
// interface consistency; acquisition is not cancellable because the critical
// sections it guards (a metadata read-modify-write) are short-lived.
func (m *MemoryLocker) Lock(_ context.Context, key string) (func(), error) {
	v, _ := m.mu.LoadOrStore(key, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock, nil
}
