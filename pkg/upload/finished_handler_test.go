package upload

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	ctxpkg "github.com/opencloud-eu/reva/v3/pkg/ctx"
	"github.com/opencloud-eu/reva/v3/pkg/storage"
)

// finishHandlerFS is a fakeFS that also wants to know when an upload has finished.
type finishHandlerFS struct {
	*fakeFS
	finished []string
}

var _ storage.UploadFinishedHandler = (*finishHandlerFS)(nil)

func (f *finishHandlerFS) HandleUploadFinished(_ context.Context, ref *provider.Reference, sessionID string, versionCreated bool) {
	id := ref.GetResourceId()
	f.finished = append(f.finished, fmt.Sprintf("%s/%s/%s versionCreated=%t", id.GetSpaceId(), id.GetOpaqueId(), sessionID, versionCreated))
}

var _ = Describe("a driver handling finished uploads", func() {
	var (
		ctx   context.Context
		store *FileStore
		fs    *finishHandlerFS
		c     *coordinator
	)

	BeforeEach(func() {
		ctx = ctxpkg.ContextSetUser(context.Background(), &userpb.User{
			Id: &userpb.UserId{OpaqueId: "alice", Idp: "idp.example.com"},
		})
		store = NewFileStore(filepath.Join(GinkgoT().TempDir(), "uploads"), TokenOptions{}, nopLog())
		Expect(store.Setup()).To(Succeed())

		fs = &finishHandlerFS{fakeFS: &fakeFS{
			touched: &provider.ResourceId{StorageId: mountID, SpaceId: spaceID, OpaqueId: nodeID},
			md: &provider.ResourceInfo{
				Id:   &provider.ResourceId{StorageId: mountID, SpaceId: spaceID, OpaqueId: nodeID},
				Size: uint64(bodyLen),
			},
		}}
		c = NewCoordinator(fs, store, "", nil)
	})

	It("is told when an upload that replaced the content has finished", func() {
		session := stagedSession(ctx, store, true)
		fs.prepared = &storage.PrepareUploadResult{VersionCreated: true}

		_, err := c.finishUpload(ctx, session)

		Expect(err).ToNot(HaveOccurred())
		Expect(fs.finished).To(Equal([]string{spaceID + "/" + nodeID + "/" + session.ID() + " versionCreated=true"}))
	})

	It("is told when an upload that created the file has finished, without a version", func() {
		session := stagedSession(ctx, store, false)

		_, err := c.finishUpload(ctx, session)

		Expect(err).ToNot(HaveOccurred())
		Expect(fs.finished).To(Equal([]string{spaceID + "/" + nodeID + "/" + session.ID() + " versionCreated=false"}))
	})

	It("is not told about an upload that was rolled back", func() {
		session := stagedSession(ctx, store, true)
		fs.prepared = &storage.PrepareUploadResult{VersionCreated: true}
		fs.commitErr = errors.New("blobstore unavailable")

		_, err := c.finishUpload(ctx, session)

		Expect(err).To(HaveOccurred())
		Expect(fs.calls).To(ContainElement(ContainSubstring("RollbackUpload")))
		Expect(fs.finished).To(BeEmpty())
	})
})
