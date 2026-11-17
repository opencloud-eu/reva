// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
)

// casMaxAttempts bounds the compare-and-swap retries in casUpload. It matches
// the retry budget the jsoncs3 share caches used before UploadWithLock.
const casMaxAttempts = 100

const (
	casMinBackoff = 5 * time.Millisecond
	casMaxBackoff = 250 * time.Millisecond
)

// casBackoff returns how long to wait before retry n (n >= 1): exponential
// from casMinBackoff up to casMaxBackoff, with jitter so that writers that
// collided do not collide again right away. It is a variable for tests.
var casBackoff = func(n int) time.Duration {
	d := min(casMinBackoff<<min(n-1, 16), casMaxBackoff)
	return d/2 + rand.N(d)
}

// casStorage is the subset of Storage that casUpload needs.
type casStorage interface {
	Download(ctx context.Context, req DownloadRequest) (*DownloadResponse, error)
	Upload(ctx context.Context, req UploadRequest) (*UploadResponse, error)
}

// casUpload performs an optimistic read-modify-write on req.Path: it downloads
// the current content, passes it to fn (nil if the file does not exist) and
// uploads the result conditionally — If-Match on the etag it read, or
// If-None-Match "*" when the file did not exist. If another writer changed the
// file in between, the cycle is retried with fresh content after a short,
// jittered backoff, so fn may be called more than once and must only depend on
// its argument. Returning nil from fn
// skips the write.
//
// A caller-supplied If-None-Match "*" in req makes the operation create-only: an
// existing file yields AlreadyExists without calling fn.
func casUpload(ctx context.Context, s casStorage, req UploadRequest, fn func(existing []byte) ([]byte, error)) (*UploadResponse, error) {
	createOnly := false
	for _, tag := range req.IfNoneMatch {
		if tag == "*" {
			createOnly = true
		}
	}

	var lastErr error
	for attempt := range casMaxAttempts {
		if attempt > 0 {
			t := time.NewTimer(casBackoff(attempt))
			select {
			case <-ctx.Done():
				t.Stop()
				return nil, fmt.Errorf("metadata: update of %s: %w (last error: %w)", req.Path, ctx.Err(), lastErr)
			case <-t.C:
			}
		}
		var existing []byte
		priorEtag := ""
		exists := false
		dres, err := s.Download(ctx, DownloadRequest{Path: req.Path})
		switch err.(type) {
		case nil:
			existing = dres.Content
			priorEtag = dres.Etag
			exists = true
		case errtypes.NotFound:
		default:
			return nil, err
		}

		if exists && createOnly {
			return nil, errtypes.AlreadyExists(req.Path)
		}

		newContent, err := fn(existing)
		if err != nil {
			return nil, err
		}
		if newContent == nil {
			// Nothing to write; report the etag we observed so callers can keep
			// their cache-invalidation token stable.
			return &UploadResponse{Etag: priorEtag}, nil
		}

		ureq := UploadRequest{Path: req.Path, Content: newContent, MTime: req.MTime}
		if exists {
			ureq.IfMatchEtag = priorEtag
		} else {
			ureq.IfNoneMatch = []string{"*"}
		}
		res, err := s.Upload(ctx, ureq)
		switch err.(type) {
		case nil:
			return res, nil
		case errtypes.Aborted, errtypes.PreconditionFailed:
			// the etag changed since we read the file
		case errtypes.AlreadyExists:
			// the file was created since we found it missing
			if createOnly {
				return nil, err
			}
		default:
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("metadata: update of %s failed after %d attempts: %w", req.Path, casMaxAttempts, lastErr)
}
