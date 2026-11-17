// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

// Package locks provides pluggable locking strategies used to make
// read-modify-write operations on metadata files atomic across replicas.
package locks

import "context"

// Locker serializes access to a named resource (a storage path).
//
// Lock blocks until the exclusive lock for key is acquired or ctx is
// cancelled. It returns an unlock function that releases the lock exactly
// once; callers typically invoke it via defer. Implementations must be safe
// for concurrent use.
type Locker interface {
	Lock(ctx context.Context, key string) (func(), error)
}
