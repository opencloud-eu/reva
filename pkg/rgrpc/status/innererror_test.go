// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package status_test

import (
	"errors"
	"fmt"

	types "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/status"
)

// detailedErr is a typed error that opts into StatusInnerErrorProvider.
type detailedErr struct {
	msg   string
	entry *types.OpaqueEntry
}

func (e *detailedErr) Error() string { return e.msg }

func (e *detailedErr) StatusInnerError() *types.OpaqueEntry { return e.entry }

var _ = Describe("InnerErrorFromErr", func() {
	entry := &types.OpaqueEntry{Decoder: "json", Value: []byte(`{"reason":"expired"}`)}

	It("returns nil for a nil error", func() {
		Expect(status.InnerErrorFromErr(nil)).To(BeNil())
	})

	It("attaches the entry from a typed error implementing the interface", func() {
		err := &detailedErr{msg: "boom", entry: entry}
		Expect(status.InnerErrorFromErr(err)).To(Equal(entry))
	})

	It("still provides details when the typed error is wrapped", func() {
		inner := &detailedErr{msg: "boom", entry: entry}
		err := fmt.Errorf("outer: %w", inner)
		Expect(status.InnerErrorFromErr(err)).To(Equal(entry))
	})

	It("returns nil for a normal error", func() {
		err := errors.New("plain error")
		Expect(status.InnerErrorFromErr(err)).To(BeNil())
	})

	It("returns nil when the provider itself returns a nil entry", func() {
		err := &detailedErr{msg: "boom", entry: nil}
		Expect(status.InnerErrorFromErr(err)).To(BeNil())
	})

	It("never copies the error text into InnerError automatically", func() {
		err := errors.New("some sensitive internal detail")
		Expect(status.InnerErrorFromErr(err)).To(BeNil())
	})
})
