// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package locks

import "context"

// NoopLocker is a Locker that does not lock at all. It is meant for callers
// whose read-modify-write cycles are already safe without a lock (e.g. because
// they use etag-conditional writes and retry) and only use a Locker to reduce
// contention.
type NoopLocker struct{}

// NewNoopLocker returns a NoopLocker.
func NewNoopLocker() NoopLocker {
	return NoopLocker{}
}

// Lock returns immediately with a no-op unlock function.
func (NoopLocker) Lock(context.Context, string) (func(), error) {
	return func() {}, nil
}
