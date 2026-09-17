// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/opencloud-eu/reva/v2/pkg/auth"
)

type ctxKeyType struct{}

var ctxKey = ctxKeyType{}

// taggedCtx returns a context carrying tag so tests can identify which context an
// authenticator produced and which one a Session currently exposes.
func taggedCtx(tag string) context.Context {
	return context.WithValue(context.Background(), ctxKey, tag)
}

func tagOf(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	tag, _ := ctx.Value(ctxKey).(string)
	return tag
}

// countingAuth returns an Authenticator that hands out contexts tagged v0, v1, …
// on successive calls. Each token expires refreshLeeway+step from now, so the
// Session's background refresher fires roughly every step. It also records how
// often it was called.
func countingAuth(step time.Duration, calls *int32) auth.Authenticator {
	return func(context.Context) (context.Context, time.Time, error) {
		n := atomic.AddInt32(calls, 1)
		return taggedCtx(fmt.Sprintf("v%d", n-1)), time.Now().Add(auth.RefreshLeeway + step), nil
	}
}

var _ = Describe("Session", func() {
	// tagFn reads the current tag of a session's context, for use with Eventually.
	tagFn := func(s *auth.Session) func() string {
		return func() string { return tagOf(s.Ctx()) }
	}
	countFn := func(calls *int32) func() int32 {
		return func() int32 { return atomic.LoadInt32(calls) }
	}

	Describe("NewSession", func() {
		It("returns an error when the initial authentication fails", func() {
			authErr := errors.New("initial auth failed")
			s, err := auth.NewSession(context.Background(), func(context.Context) (context.Context, time.Time, error) {
				return nil, time.Time{}, authErr
			})

			Expect(err).To(MatchError(authErr))
			Expect(s).To(BeNil())
		})

		It("exposes the initial context via Ctx", func() {
			initial := taggedCtx("initial")
			s, err := auth.NewSession(context.Background(), func(context.Context) (context.Context, time.Time, error) {
				return initial, time.Now().Add(time.Hour), nil
			})
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(s.Close)

			Expect(s.Ctx()).To(Equal(initial))
		})

		It("refreshes the context before the token expires", func() {
			var calls int32
			s, err := auth.NewSession(context.Background(), countingAuth(40*time.Millisecond, &calls))
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(s.Close)

			Expect(tagOf(s.Ctx())).To(Equal("v0"))
			Eventually(tagFn(s), "2s", "10ms").Should(Equal("v1"))
		})

		It("keeps refreshing repeatedly", func() {
			var calls int32
			s, err := auth.NewSession(context.Background(), countingAuth(30*time.Millisecond, &calls))
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(s.Close)

			Eventually(tagFn(s), "2s", "10ms").Should(Equal("v1"))
			Eventually(tagFn(s), "2s", "10ms").Should(Equal("v2"))
			Eventually(tagFn(s), "2s", "10ms").Should(Equal("v3"))
		})

		It("keeps the current context when a refresh fails", func() {
			var calls int32
			s, err := auth.NewSession(context.Background(), func(context.Context) (context.Context, time.Time, error) {
				n := atomic.AddInt32(&calls, 1)
				if n == 1 {
					return taggedCtx("v0"), time.Now().Add(auth.RefreshLeeway + 20*time.Millisecond), nil
				}
				return nil, time.Time{}, errors.New("refresh failed")
			})
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(s.Close)

			// wait until the (failing) refresh has been attempted at least once
			Eventually(countFn(&calls), "2s", "10ms").Should(BeNumerically(">=", 2))
			// the failed refresh must not clear or replace the valid context
			Consistently(tagFn(s), "300ms", "20ms").Should(Equal("v0"))
		})

		It("stops refreshing after Close", func() {
			var calls int32
			s, err := auth.NewSession(context.Background(), countingAuth(30*time.Millisecond, &calls))
			Expect(err).ToNot(HaveOccurred())

			Eventually(countFn(&calls), "2s", "10ms").Should(BeNumerically(">=", 2))
			s.Close()

			// at most one refresh may already have been in flight when Close returned
			countAfterClose := atomic.LoadInt32(&calls)
			Consistently(countFn(&calls), "300ms", "20ms").Should(BeNumerically("<=", countAfterClose+1))
		})

		It("stops refreshing when the parent context is cancelled", func() {
			var calls int32
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()

			s, err := auth.NewSession(parent, countingAuth(30*time.Millisecond, &calls))
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(s.Close)

			Eventually(countFn(&calls), "2s", "10ms").Should(BeNumerically(">=", 2))
			cancel()

			countAfterCancel := atomic.LoadInt32(&calls)
			Consistently(countFn(&calls), "300ms", "20ms").Should(BeNumerically("<=", countAfterCancel+1))
		})
	})

	Describe("NewStaticSession", func() {
		It("always returns the provided context and never refreshes", func() {
			ctx := taggedCtx("static")
			s := auth.NewStaticSession(ctx)

			Expect(s.Ctx()).To(Equal(ctx))
			Consistently(tagFn(s), "200ms", "20ms").Should(Equal("static"))
		})

		It("can be closed safely and repeatedly", func() {
			s := auth.NewStaticSession(taggedCtx("static"))

			Expect(func() { s.Close() }).ToNot(Panic())
			Expect(func() { s.Close() }).ToNot(Panic())
		})
	})

	Describe("Close", func() {
		It("is idempotent on a refreshing session", func() {
			var calls int32
			s, err := auth.NewSession(context.Background(), countingAuth(30*time.Millisecond, &calls))
			Expect(err).ToNot(HaveOccurred())

			Expect(func() { s.Close() }).ToNot(Panic())
			Expect(func() { s.Close() }).ToNot(Panic())
		})
	})

	It("is safe for concurrent readers while refreshing", func() {
		var calls int32
		// step of 1ms keeps refreshes frequent (writes) without hot-spinning
		s, err := auth.NewSession(context.Background(), countingAuth(time.Millisecond, &calls))
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(s.Close)

		var wg sync.WaitGroup
		for range 16 {
			wg.Go(func() {
				for range 2000 {
					_ = s.Ctx()
				}
			})
		}
		wg.Wait()

		Expect(s.Ctx()).ToNot(BeNil())
	})
})
