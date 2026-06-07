// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package tus

import (
	"context"
	"errors"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	tusd "github.com/tus/tusd/v2/pkg/handler"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/opencloud-eu/reva/v2/internal/http/services/owncloud/ocdav/net"
	ctxpkg "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
)

// fakeFS implements only GetMD; embedding storage.FS satisfies the rest of the
// interface (those methods are never called by the callback under test). It records
// the reference and the context user it was called with so the tests can assert how
// the callback resolves and authenticates the stat.
type fakeFS struct {
	storage.FS
	md         *provider.ResourceInfo
	err        error
	gotRef     *provider.Reference
	gotUserID  string
	gotUserIdp string
}

func (f *fakeFS) GetMD(ctx context.Context, ref *provider.Reference, _, _ []string) (*provider.ResourceInfo, error) {
	f.gotRef = ref
	if u, ok := ctxpkg.ContextGetUser(ctx); ok {
		f.gotUserID = u.GetId().GetOpaqueId()
		f.gotUserIdp = u.GetId().GetIdp()
	}
	return f.md, f.err
}

var _ = Describe("executantFromUploadInfo", func() {
	It("maps the stored session keys onto a user", func() {
		u := executantFromUploadInfo(tusd.FileInfo{Storage: map[string]string{
			"UserType":        "USER_TYPE_PRIMARY",
			"Idp":             "https://idp.example",
			"UserId":          "opaque-123",
			"UserName":        "alice",
			"UserDisplayName": "Alice",
		}})
		Expect(u.GetId().GetOpaqueId()).To(Equal("opaque-123"))
		Expect(u.GetId().GetIdp()).To(Equal("https://idp.example"))
		Expect(u.GetId().GetType()).To(Equal(userpb.UserType_USER_TYPE_PRIMARY))
		Expect(u.GetUsername()).To(Equal("alice"))
		Expect(u.GetDisplayName()).To(Equal("Alice"))
	})

	It("does not panic on empty info and yields an invalid user type", func() {
		u := executantFromUploadInfo(tusd.FileInfo{Storage: map[string]string{}})
		Expect(u.GetId().GetType()).To(Equal(userpb.UserType_USER_TYPE_INVALID))
		Expect(u.GetId().GetOpaqueId()).To(BeEmpty())
	})
})

var _ = Describe("preFinishResponseCallback", func() {
	// the session keys the storage writes at upload-create time
	hookFor := func() tusd.HookEvent {
		return tusd.HookEvent{
			Context: context.Background(),
			Upload: tusd.FileInfo{
				MetaData: map[string]string{"providerID": "provider-1"},
				Storage: map[string]string{
					"SpaceRoot": "space-1",
					"NodeId":    "node-1",
					"UserId":    "executant-1",
					"Idp":       "https://idp.example",
					"UserType":  "USER_TYPE_PRIMARY",
				},
			},
		}
	}

	// a resource the executant may edit; owned by someone else unless overridden
	editable := func(etag, ownerID string) *provider.ResourceInfo {
		return &provider.ResourceInfo{
			Etag:  etag,
			Type:  provider.ResourceType_RESOURCE_TYPE_FILE,
			Owner: &userpb.UserId{Idp: "https://idp.example", OpaqueId: ownerID},
			PermissionSet: &provider.ResourcePermissions{
				Stat: true, ListContainer: true, InitiateFileDownload: true,
				InitiateFileUpload: true, Move: true, Delete: true,
			},
		}
	}

	It("stats the finalized node as the executant and sets etag and permissions", func() {
		fs := &fakeFS{md: editable(`"abc123"`, "other-owner")}

		resp, err := (&manager{}).preFinishResponseCallback(fs)(hookFor())

		Expect(err).ToNot(HaveOccurred())
		// the reference is reconstructed from the session keys
		Expect(fs.gotRef.GetResourceId().GetStorageId()).To(Equal("provider-1"))
		Expect(fs.gotRef.GetResourceId().GetSpaceId()).To(Equal("space-1"))
		Expect(fs.gotRef.GetResourceId().GetOpaqueId()).To(Equal("node-1"))
		// the stat runs with the executant's identity restored into the context
		Expect(fs.gotUserID).To(Equal("executant-1"))
		Expect(fs.gotUserIdp).To(Equal("https://idp.example"))
		// etag mirrored into both headers
		Expect(resp.Header[net.HeaderOCETag]).To(Equal(`"abc123"`))
		Expect(resp.Header[net.HeaderETag]).To(Equal(`"abc123"`))
		// not owned by the executant -> shared -> WebDAV permissions lead with S
		Expect(resp.Header[net.HeaderOCPermissions]).To(HavePrefix("S"))
	})

	It("omits the shared marker when the executant owns the resource", func() {
		fs := &fakeFS{md: editable(`"abc123"`, "executant-1")}

		resp, err := (&manager{}).preFinishResponseCallback(fs)(hookFor())

		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Header[net.HeaderOCPermissions]).ToNot(BeEmpty())
		Expect(resp.Header[net.HeaderOCPermissions]).ToNot(HavePrefix("S"))
	})

	It("is best-effort: no headers and no error when the stat fails", func() {
		fs := &fakeFS{err: errors.New("not found")}

		resp, err := (&manager{}).preFinishResponseCallback(fs)(hookFor())

		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Header).ToNot(HaveKey(net.HeaderOCETag))
		Expect(resp.Header).ToNot(HaveKey(net.HeaderETag))
		Expect(resp.Header).ToNot(HaveKey(net.HeaderOCPermissions))
	})

	It("is best-effort: no headers and no error when the stat returns no resource", func() {
		fs := &fakeFS{md: nil, err: nil}

		resp, err := (&manager{}).preFinishResponseCallback(fs)(hookFor())

		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Header).ToNot(HaveKey(net.HeaderOCPermissions))
		Expect(resp.Header).ToNot(HaveKey(net.HeaderOCETag))
	})

	It("omits the etag header when the resource has none", func() {
		fs := &fakeFS{md: editable("", "other-owner")}

		resp, err := (&manager{}).preFinishResponseCallback(fs)(hookFor())

		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Header).ToNot(HaveKey(net.HeaderOCETag))
		Expect(resp.Header).To(HaveKey(net.HeaderOCPermissions))
	})
})
