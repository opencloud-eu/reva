// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package tus

import (
	"context"
	"errors"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/rs/zerolog"
	tusd "github.com/tus/tusd/v2/pkg/handler"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/opencloud-eu/reva/v2/internal/http/services/owncloud/ocdav/net"
	ctxpkg "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
)

// fakeResolver implements storage.UploadSessionResolver. Embedding storage.FS satisfies the
// rest of that interface (those methods are never called by the callback under test). It
// records the upload info it was called with and returns a preconfigured resource, context
// and error, so the tests can drive the callback without a real storage backend.
type fakeResolver struct {
	storage.FS
	ri      *provider.ResourceInfo
	ctx     context.Context
	err     error
	gotInfo tusd.FileInfo
}

func (f *fakeResolver) ResolveUpload(ctx context.Context, info tusd.FileInfo) (*provider.ResourceInfo, context.Context, error) {
	f.gotInfo = info
	rctx := f.ctx
	if rctx == nil {
		rctx = ctx
	}
	return f.ri, rctx, f.err
}

// plainFS implements only storage.FS, not the resolver, so the callback takes its no-op path.
type plainFS struct{ storage.FS }

// testManager builds a manager with a no-op logger, the way New sets one in production, so the
// callback's warn path does not dereference a nil logger.
func testManager() *manager {
	l := zerolog.Nop()
	return &manager{log: &l}
}

var _ = Describe("preFinishResponseCallback", func() {
	executant := &userpb.UserId{Idp: "https://idp.example", OpaqueId: "executant-1"}
	asExecutant := ctxpkg.ContextSetUser(context.Background(), &userpb.User{Id: executant})

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

	hook := tusd.HookEvent{
		Context: context.Background(),
		Upload:  tusd.FileInfo{ID: "upload-1", Storage: map[string]string{"NodeId": "node-1"}},
	}

	It("resolves the finished upload and sets etag and permissions", func() {
		fs := &fakeResolver{ri: editable(`"abc123"`, "other-owner"), ctx: asExecutant}

		resp, err := testManager().preFinishResponseCallback(fs)(hook)

		Expect(err).ToNot(HaveOccurred())
		// the in-hand tus info is handed to the resolver, not looked up by id in the session store
		Expect(fs.gotInfo.ID).To(Equal("upload-1"))
		// etag mirrored into both headers
		Expect(resp.Header[net.HeaderOCETag]).To(Equal(`"abc123"`))
		Expect(resp.Header[net.HeaderETag]).To(Equal(`"abc123"`))
		// resolved as a user who does not own the resource -> shared -> permissions lead with S
		Expect(resp.Header[net.HeaderOCPermissions]).To(HavePrefix("S"))
	})

	It("omits the shared marker when the resource is owned by the resolving user", func() {
		fs := &fakeResolver{ri: editable(`"abc123"`, "executant-1"), ctx: asExecutant}

		resp, err := testManager().preFinishResponseCallback(fs)(hook)

		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Header[net.HeaderOCPermissions]).ToNot(BeEmpty())
		Expect(resp.Header[net.HeaderOCPermissions]).ToNot(HavePrefix("S"))
	})

	It("is best-effort: no headers and no error when the resolve fails", func() {
		fs := &fakeResolver{err: errors.New("not found"), ctx: asExecutant}

		resp, err := testManager().preFinishResponseCallback(fs)(hook)

		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Header).ToNot(HaveKey(net.HeaderOCETag))
		Expect(resp.Header).ToNot(HaveKey(net.HeaderETag))
		Expect(resp.Header).ToNot(HaveKey(net.HeaderOCPermissions))
	})

	It("is best-effort: no headers and no error when the resolve returns no resource", func() {
		fs := &fakeResolver{ri: nil, ctx: asExecutant}

		resp, err := testManager().preFinishResponseCallback(fs)(hook)

		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Header).ToNot(HaveKey(net.HeaderOCPermissions))
		Expect(resp.Header).ToNot(HaveKey(net.HeaderOCETag))
	})

	It("omits the etag header when the resource has none", func() {
		fs := &fakeResolver{ri: editable("", "other-owner"), ctx: asExecutant}

		resp, err := testManager().preFinishResponseCallback(fs)(hook)

		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Header).ToNot(HaveKey(net.HeaderOCETag))
		Expect(resp.Header).To(HaveKey(net.HeaderOCPermissions))
	})

	It("does nothing when the storage driver cannot resolve uploads", func() {
		resp, err := testManager().preFinishResponseCallback(plainFS{})(hook)

		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Header).ToNot(HaveKey(net.HeaderOCETag))
		Expect(resp.Header).ToNot(HaveKey(net.HeaderOCPermissions))
	})
})
