// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package decomposedfs

import (
	"bytes"
	"context"
	"io"
	"os"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	cs3permissions "github.com/cs3org/go-cs3apis/cs3/permissions/v1beta1"
	v1beta11 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"

	ruser "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/events"
	"github.com/opencloud-eu/reva/v2/pkg/events/stream"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/aspects"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/lookup"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata"
	nodemocks "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node/mocks"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/options"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/permissions"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/permissions/mocks"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/revisioncleanup"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/timemanager"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/tree"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	"github.com/opencloud-eu/reva/v2/pkg/store"
	"github.com/opencloud-eu/reva/v2/tests/helpers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The revision cleanup trigger of uploads with async postprocessing: the cleanup runs once
// postprocessing finished successfully (see Decomposedfs.handlePostprocessingEvent), with a
// strategy that deletes all revisions, so that the effect is easy to see. The setup follows
// upload_async_test.go, with the revision cleanup wired in.
var _ = Describe("Revision cleanup of async uploads", func() {
	var (
		user = &userpb.User{
			Id:       &userpb.UserId{Idp: "idp", OpaqueId: "u-s-e-r-id", Type: userpb.UserType_USER_TYPE_PRIMARY},
			Username: "username",
		}
		ctx = ruser.ContextSetUser(context.Background(), user)

		ref      *provider.Reference
		o        *options.Options
		lu       *lookup.Lookup
		tp       *tree.Tree
		fs       storage.FS
		bs       *nodemocks.Blobstore
		pub, con chan any
	)

	BeforeEach(func() {
		ref = &provider.Reference{ResourceId: &provider.ResourceId{SpaceId: "u-s-e-r-id"}, Path: "/foo"}
		tmpRoot, err := helpers.TempDir("reva-unit-tests-*-root")
		Expect(err).ToNot(HaveOccurred())

		o, err = options.New(map[string]any{
			"root":                       tmpRoot,
			"asyncfileuploads":           true,
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

		bs = &nodemocks.Blobstore{}
		bs.On("Upload", mock.Anything, mock.Anything, mock.Anything).Return(nil)
		bs.On("Delete", mock.Anything).Return(nil)

		pub, con = make(chan any), make(chan any)
		tp = tree.New(lu, bs, o, p, store.Create(), &zerolog.Logger{})
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

	// uploads content and finishes its postprocessing with the given outcome
	upload := func(content []byte, outcome events.PostprocessingOutcome) {
		uploadIds, err := fs.InitiateUpload(ctx, ref, int64(len(content)), map[string]string{})
		Expect(err).ToNot(HaveOccurred())
		_, err = fs.Upload(ctx, storage.UploadRequest{
			Ref:    &provider.Reference{Path: "/" + uploadIds["simple"]},
			Body:   io.NopCloser(bytes.NewReader(content)),
			Length: int64(len(content)),
		}, nil)
		Expect(err).ToNot(HaveOccurred())
		_, ok := (<-pub).(events.BytesReceived)
		Expect(ok).To(BeTrue())

		con <- events.PostprocessingFinished{UploadID: uploadIds["simple"], Outcome: outcome}
		// the cleanup runs before UploadReady is published
		_, ok = (<-pub).(events.UploadReady)
		Expect(ok).To(BeTrue())
	}

	revisionCount := func() int {
		revs, err := fs.ListRevisions(ctx, ref)
		Expect(err).ToNot(HaveOccurred())
		return len(revs)
	}

	It("cleans up once postprocessing of an upload that replaced the file succeeded", func() {
		upload([]byte("first"), events.PPOutcomeContinue)
		bs.AssertNotCalled(GinkgoT(), "Delete", mock.Anything)

		upload([]byte("second"), events.PPOutcomeContinue)

		Expect(revisionCount()).To(Equal(0))
		// the blob of the first content went with its revision
		bs.AssertNumberOfCalls(GinkgoT(), "Delete", 1)
	})

	It("does not clean up after failed postprocessing, which restores the previous content instead", func() {
		upload([]byte("first"), events.PPOutcomeContinue)
		// an old revision that a cleanup with "0" would delete
		n, err := lu.NodeFromResource(ctx, ref)
		Expect(err).ToNot(HaveOccurred())
		_, err = tp.CreateRevision(ctx, n, "2020-01-01T00:00:00Z")
		Expect(err).ToNot(HaveOccurred())
		Expect(revisionCount()).To(Equal(1))

		upload([]byte("second"), events.PPOutcomeDelete)

		// the failed upload's revision became the current content again, and no cleanup
		// ran: the old revision and all blobs are still there
		Expect(revisionCount()).To(Equal(1))
		for _, call := range bs.Calls {
			Expect(call.Method).ToNot(Equal("Delete"), "no blob may be deleted when postprocessing failed")
		}
	})
})
