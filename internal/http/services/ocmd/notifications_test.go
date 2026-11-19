// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package ocmd

import (
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Notifications", func() {
	// the handler has no gateway selector, so every request must be
	// rejected before the handler tries to talk to the gateway
	var h *notifHandler

	BeforeEach(func() {
		h = &notifHandler{}
	})

	notify := func(contentType, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/notifications", strings.NewReader(body))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		h.Notifications(w, req)
		return w
	}

	DescribeTable("rejects invalid notifications",
		func(contentType, body string) {
			w := notify(contentType, body)
			Expect(w.Code).To(Equal(http.StatusBadRequest))
		},
		Entry("without content type", "", `{}`),
		Entry("with a non json content type", "text/plain", `{}`),
		Entry("with invalid json", "application/json", `{`),
		Entry("without notification", "application/json",
			`{"notificationType":"SHARE_UNSHARED","resourceType":"file","providerId":"share-id"}`),
		Entry("without grantee", "application/json",
			`{"notificationType":"SHARE_UNSHARED","resourceType":"file","providerId":"share-id","notification":{}}`),
		Entry("with an unknown notification type", "application/json",
			`{"notificationType":"SHARE_ACCEPTED","resourceType":"file","providerId":"share-id","notification":{"grantee":"einstein@cernbox.cern.ch"}}`),
		Entry("changing permissions without protocol", "application/json",
			`{"notificationType":"SHARE_CHANGE_PERMISSION","resourceType":"file","providerId":"share-id","notification":{"grantee":"einstein@cernbox.cern.ch"}}`),
	)
})
