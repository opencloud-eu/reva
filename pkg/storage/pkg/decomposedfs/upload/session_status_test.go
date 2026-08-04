// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package upload_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rs/zerolog"

	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/aspects"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/options"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/upload"
)

var _ = Describe("Session", func() {
	var session *upload.DecomposedFsSession

	BeforeEach(func() {
		log := &zerolog.Logger{}
		root := GinkgoT().TempDir()
		store := upload.NewSessionStore(nil, aspects.Aspects{}, root, false, options.TokenOptions{}, log)
		session = store.New(context.Background())
	})

	Describe("a newly created session", func() {
		It("starts in the uploading status", func() {
			Expect(session.Status()).To(Equal(upload.SessionStatusUploading))
			Expect(session.StatusMessage()).To(BeEmpty())
			Expect(session.IsProcessing()).To(BeFalse())
		})
	})

	Describe("SetStatus", func() {
		It("stores the status and message", func() {
			session.SetStatus(upload.SessionStatusFailed, "something went wrong")

			Expect(session.Status()).To(Equal(upload.SessionStatusFailed))
			Expect(session.StatusMessage()).To(Equal("something went wrong"))
		})

		It("overwrites the previous message", func() {
			session.SetStatus(upload.SessionStatusFailed, "first error")
			session.SetStatus(upload.SessionStatusProcessing, "")

			Expect(session.Status()).To(Equal(upload.SessionStatusProcessing))
			Expect(session.StatusMessage()).To(BeEmpty())
		})
	})

	Describe("IsProcessing", func() {
		DescribeTable("reports the processing state based on the status",
			func(status string, wantProcessing bool) {
				session.SetStatus(status, "")

				Expect(session.IsProcessing()).To(Equal(wantProcessing))
			},
			Entry("uploading is not processing", upload.SessionStatusUploading, false),
			Entry("processing is processing", upload.SessionStatusProcessing, true),
			Entry("failed is not processing", upload.SessionStatusFailed, false),
		)
	})
})
