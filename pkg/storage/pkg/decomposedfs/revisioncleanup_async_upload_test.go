// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package decomposedfs

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	cs3permissions "github.com/cs3org/go-cs3apis/cs3/permissions/v1beta1"
	v1beta11 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"

	ruser "github.com/opencloud-eu/reva/v3/pkg/ctx"
	"github.com/opencloud-eu/reva/v3/pkg/events"
	"github.com/opencloud-eu/reva/v3/pkg/events/stream"
	"github.com/opencloud-eu/reva/v3/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v3/pkg/storage"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/aspects"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/lookup"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/metadata"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/node"
	nodemocks "github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/node/mocks"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/options"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/permissions"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/permissions/mocks"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/revisioncleanup"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/timemanager"
	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/tree"
	"github.com/opencloud-eu/reva/v3/pkg/storagespace"
	"github.com/opencloud-eu/reva/v3/pkg/store"
	uploadcoordinator "github.com/opencloud-eu/reva/v3/pkg/upload"
	"github.com/opencloud-eu/reva/v3/tests/helpers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The revision cleanup trigger of uploads with async postprocessing: the upload coordinator
// commits the upload once postprocessing succeeded and then clears the processing mark of its
// session, which triggers the cleanup (see Decomposedfs.MarkProcessing). The strategy deletes
// all revisions, so that the effect is easy to see. The setup follows upload_async_test.go,
// with the revision cleanup wired in.
var _ = Describe("Revision cleanup of async uploads", func() {
	var (
		user = &userpb.User{
			Id:       &userpb.UserId{Idp: "idp", OpaqueId: "u-s-e-r-id", Type: userpb.UserType_USER_TYPE_PRIMARY},
			Username: "username",
		}
		ctx = ruser.ContextSetUser(context.Background(), user)

		ref         *provider.Reference
		o           *options.Options
		lu          *lookup.Lookup
		fs          storage.FS
		coordinator uploadcoordinator.Coordinator
		pub, con    chan any

		deletedMu sync.Mutex
		deleted   []string // the blob ids deleted from the blobstore
		written   []string // the blob ids written to the blobstore
	)

	BeforeEach(func() {
		deleted, written = nil, nil
		ref = &provider.Reference{ResourceId: &provider.ResourceId{SpaceId: "u-s-e-r-id"}, Path: "/foo"}
		tmpRoot, err := helpers.TempDir("reva-unit-tests-*-root")
		Expect(err).ToNot(HaveOccurred())

		o, err = options.New(map[string]any{
			"root":                       tmpRoot,
			"revisions_cleanup_strategy": "0", // deletes all revisions
			"revisions_cleanup_async":    false,
			"filemetadatacache":          map[string]any{"cache_database": tmpRoot},
		})
		Expect(err).ToNot(HaveOccurred())

		lu = lookup.New(metadata.NewMessagePackBackend(o.FileMetadataCache), o, &timemanager.Manager{})
		pmock := &mocks.PermissionsChecker{}
		cs3permissionsclient := &mocks.CS3PermissionsClient{}
		pool.RemoveSelector("PermissionsSelector" + "any")
		permissionsSelector := pool.GetSelector("PermissionsSelector", "any",
			func(cc grpc.ClientConnInterface) cs3permissions.PermissionsAPIClient { return cs3permissionsclient })
		cs3permissionsclient.On("CheckPermission", mock.Anything, mock.Anything, mock.Anything).Return(&cs3permissions.CheckPermissionResponse{
			Status: &v1beta11.Status{Code: v1beta11.Code_CODE_OK},
		}, nil).Times(1)
		pmock.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
			Stat:               true,
			GetQuota:           true,
			InitiateFileUpload: true,
			ListContainer:      true,
			ListFileVersions:   true,
		}, nil)
		p := permissions.NewPermissions(pmock, permissionsSelector)

		bs := &nodemocks.Blobstore{}
		bs.On("Upload", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			deletedMu.Lock()
			defer deletedMu.Unlock()
			written = append(written, args.Get(0).(*node.Node).BlobID)
		}).Return(nil)
		bs.On("Delete", mock.Anything).Run(func(args mock.Arguments) {
			deletedMu.Lock()
			defer deletedMu.Unlock()
			deleted = append(deleted, args.Get(0).(*node.Node).BlobID)
		}).Return(nil)

		pub, con = make(chan any), make(chan any)
		tp := tree.New(lu, bs, o, p, store.Create(), &zerolog.Logger{})
		revisionsCleanup, err := revisioncleanup.NewServiceFromOptions(o, lu, tp, nil)
		Expect(err).ToNot(HaveOccurred())
		fs, err = New(o, aspects.Aspects{
			Lookup:           lu,
			Tree:             tp,
			Permissions:      p,
			EventStream:      stream.Chan{pub, con},
			Trashbin:         &DecomposedfsTrashbin{},
			RevisionsCleanup: revisionsCleanup,
		}, &zerolog.Logger{})
		Expect(err).ToNot(HaveOccurred())

		resp, err := fs.CreateStorageSpace(ctx, &provider.CreateStorageSpaceRequest{Owner: user, Type: "personal"})
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Status.Code).To(Equal(v1beta11.Code_CODE_OK))
		resID, err := storagespace.ParseID(resp.StorageSpace.Id.OpaqueId)
		Expect(err).ToNot(HaveOccurred())
		ref.ResourceId = &resID

		// the upload coordinator drives the upload and consumes the postprocessing results
		sessionStore := uploadcoordinator.NewFileStore(filepath.Join(tmpRoot, "uploads"), uploadcoordinator.TokenOptions{
			DownloadEndpoint:     "http://localhost/data",
			TransferSharedSecret: "transfer-secret",
			TransferExpires:      60,
		}, &zerolog.Logger{})
		Expect(sessionStore.Setup()).To(Succeed())
		coordinator = uploadcoordinator.NewCoordinator(fs, sessionStore, "", stream.Chan{pub, con})
		Expect(coordinator.StartPostprocessing(stream.Chan{pub, con}, uploadcoordinator.AsyncConf{ConsumerGroup: "dcfs", NumConsumers: 1})).To(Succeed())
	})

	AfterEach(func() {
		if fs != nil {
			_ = fs.Shutdown(context.Background())
		}
		if o != nil && o.Root != "" {
			_ = os.RemoveAll(o.Root)
		}
		close(pub)
		close(con)
	})

	deletedBlobs := func() []string {
		deletedMu.Lock()
		defer deletedMu.Unlock()
		return append([]string{}, deleted...)
	}
	writtenBlobs := func() []string {
		deletedMu.Lock()
		defer deletedMu.Unlock()
		return append([]string{}, written...)
	}

	// starts the upload of content, up to postprocessing, and returns its id, which is also
	// the id of its blob
	startUpload := func(content []byte) string {
		uploadIds, err := coordinator.InitiateUpload(ctx, ref, int64(len(content)), map[string]string{})
		Expect(err).ToNot(HaveOccurred())
		_, err = coordinator.Upload(ctx, uploadcoordinator.Request{
			Ref:    &provider.Reference{Path: "/" + uploadIds["simple"]},
			Body:   io.NopCloser(bytes.NewReader(content)),
			Length: int64(len(content)),
		}, nil)
		Expect(err).ToNot(HaveOccurred())
		_, ok := (<-pub).(events.BytesReceived)
		Expect(ok).To(BeTrue())
		return uploadIds["simple"]
	}

	// finishes the postprocessing of an upload with the given outcome
	finishPostprocessing := func(uploadID string, outcome events.PostprocessingOutcome) {
		con <- events.PostprocessingFinished{UploadID: uploadID, Outcome: outcome}
		// the cleanup runs before UploadReady is published
		ev, ok := (<-pub).(events.UploadReady)
		Expect(ok).To(BeTrue())
		Expect(ev.Failed).To(Equal(outcome != events.PPOutcomeContinue))
	}

	revisionCount := func() int {
		revs, err := fs.ListRevisions(ctx, ref)
		Expect(err).ToNot(HaveOccurred())
		return len(revs)
	}

	blobID := func() string {
		n, err := lu.NodeFromResource(ctx, ref)
		Expect(err).ToNot(HaveOccurred())
		return n.BlobID
	}

	It("cleans up once postprocessing of an upload that replaced the file succeeded", func() {
		first := startUpload([]byte("first"))
		finishPostprocessing(first, events.PPOutcomeContinue)
		Expect(deletedBlobs()).To(BeEmpty())

		second := startUpload([]byte("second"))
		// not while the upload is in postprocessing: it may still be rolled back to the
		// revision of the first content
		Expect(revisionCount()).To(Equal(1))
		Expect(deletedBlobs()).To(BeEmpty())

		finishPostprocessing(second, events.PPOutcomeContinue)

		Expect(revisionCount()).To(Equal(0))
		Expect(blobID()).To(Equal(second))
		// the blob of the first content went with its revision
		Expect(deletedBlobs()).To(ConsistOf(first))
	})

	It("leaves the revision alone that an older upload still writes into, until it is a version", func() {
		first := startUpload([]byte("first"))
		finishPostprocessing(first, events.PPOutcomeContinue)
		older := startUpload([]byte("older"))

		// An empty file is never postprocessed: the coordinator finishes its upload right away,
		// which clears the processing mark of the file and triggers the cleanup, while the
		// older upload is still in postprocessing. The revision of the older upload keeps its
		// processing mark, the older upload writes its content into it once it is done.
		uploaded := make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(uploaded)
			// an empty upload is finished when it is initiated
			_, err := coordinator.InitiateUpload(ctx, ref, 0, map[string]string{})
			Expect(err).ToNot(HaveOccurred())
		}()
		ev, ok := (<-pub).(events.UploadReady)
		Expect(ok).To(BeTrue())
		Expect(ev.Failed).To(BeFalse())
		Eventually(uploaded).Should(BeClosed())

		// the cleanup deleted the version of the first content, not the older upload's revision
		Expect(deletedBlobs()).To(ConsistOf(first))
		Expect(revisionCount()).To(Equal(1))

		Expect(writtenBlobs()).ToNot(ContainElement(older))

		finishPostprocessing(older, events.PPOutcomeContinue)

		// The older upload wrote its content into its revision, which made it a version, and
		// the cleanup ran once more for it: the strategy deletes all versions. Without that,
		// it would stay until the file gets its next revision.
		Expect(writtenBlobs()).To(ContainElement(older))
		Expect(revisionCount()).To(Equal(0))
		Expect(deletedBlobs()).To(ConsistOf(first, older))
	})

	It("keeps the previous content after failed postprocessing", func() {
		first := startUpload([]byte("first"))
		finishPostprocessing(first, events.PPOutcomeContinue)

		second := startUpload([]byte("second"))
		finishPostprocessing(second, events.PPOutcomeDelete)

		// the rollback restored the first content from the revision the failed upload
		// created, so there is no revision left, and the blob of the first content is kept
		Expect(blobID()).To(Equal(first))
		Expect(revisionCount()).To(Equal(0))
		Expect(deletedBlobs()).ToNot(ContainElement(first))
	})
})
