// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisions

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

type Revision interface {
	Key() string
	LastModificationTime() time.Time
	Size() uint64
}

type Plan interface {
	Delete(...Revision)
	Retain(...Revision)
	Safeguard(...Revision)
	Deleted() []Revision
	Retained() []Revision
	Safeguarded() []Revision
}

type RevisionCleanupStrategy interface {
	Cleanup(now time.Time, revs []Revision, ctx context.Context, logger *zerolog.Logger) (Plan, error)
}
