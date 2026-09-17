// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"sync"
	"time"
)

const (
	// RefreshLeeway is how long before a token's expiry a Session refreshes it.
	// Refreshing ahead of time avoids races where a request is made with a token
	// that expires while it is in flight.
	RefreshLeeway = 30 * time.Second

	retryBackoff = 5 * time.Second
)

// Authenticator produces an authenticated context together with the time at which
// its token expires. A Session calls it once up front and then again shortly
// before each expiry to keep the context fresh.
type Authenticator func(ctx context.Context) (context.Context, time.Time, error)

// Session keeps an authenticated context valid for the whole duration of a
// long-running task.
//
// Many operations (indexing a space, creating an archive, bulk migrations, …)
// can outlive the lifetime of a single authentication token. Passing a static
// context to such a task means its token expires halfway through and every
// subsequent call fails. Session solves this by owning the context: it
// authenticates once up front via an Authenticator and then refreshes the
// context in the background shortly before the token expires. Callers always
// read the current context through Ctx, so they transparently pick up refreshed
// tokens without having to thread a new context around.
// A Session is safe for concurrent use.
type Session struct {
	mu   sync.RWMutex
	ctx  context.Context
	stop chan struct{}
	once sync.Once
}

// NewSession authenticates once via auth and then refreshes the context in the
// background shortly before the token expires. It returns an error if the initial
// authentication fails.
func NewSession(parent context.Context, auth Authenticator) (*Session, error) {
	ctx, expiry, err := auth(parent)
	if err != nil {
		return nil, err
	}
	s := &Session{ctx: ctx, stop: make(chan struct{})}
	go s.refresh(parent, auth, expiry)
	return s, nil
}

// NewStaticSession returns a Session that always returns ctx and never refreshes.
// It is useful for callers that manage their own (typically short-lived) context
// but need to satisfy a Session-based API.
func NewStaticSession(ctx context.Context) *Session {
	return &Session{ctx: ctx, stop: make(chan struct{})}
}

// Ctx returns the current authentication context. It always carries a valid token
// as long as the Authenticator keeps succeeding.
func (s *Session) Ctx() context.Context {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ctx
}

// Close stops the background refresher. It is safe to call more than once and on
// a static session.
func (s *Session) Close() {
	s.once.Do(func() { close(s.stop) })
}

func (s *Session) set(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctx = ctx
}

func (s *Session) refresh(parent context.Context, auth Authenticator, expiry time.Time) {
	for {
		wait := max(time.Until(expiry)-RefreshLeeway, 0)

		timer := time.NewTimer(wait)
		select {
		case <-s.stop:
			timer.Stop()
			return
		case <-parent.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		ctx, exp, err := auth(parent)
		if err != nil {
			// keep using the current context and retry after a short backoff
			select {
			case <-s.stop:
				return
			case <-parent.Done():
				return
			case <-time.After(retryBackoff):
			}
			continue
		}
		s.set(ctx)
		expiry = exp
	}
}
