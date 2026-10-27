package decomposedfs

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	cs3permissions "github.com/cs3org/go-cs3apis/cs3/permissions/v1beta1"
	v1beta11 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	ruser "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/events"
	"github.com/opencloud-eu/reva/v2/pkg/events/stream"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/aspects"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/lookup"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	nodemocks "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node/mocks"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/options"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/permissions"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/permissions/mocks"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/timemanager"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/tree"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	"github.com/opencloud-eu/reva/v2/pkg/store"
	uploadcoordinator "github.com/opencloud-eu/reva/v2/pkg/upload"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
	"github.com/opencloud-eu/reva/v2/tests/helpers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Async file uploads", Ordered, func() {
	var (
		ref = &provider.Reference{
			ResourceId: &provider.ResourceId{
				SpaceId: "u-s-e-r-id",
			},
			Path: "/foo",
		}

		rootRef = &provider.Reference{
			ResourceId: &provider.ResourceId{
				SpaceId:  "u-s-e-r-id",
				OpaqueId: "u-s-e-r-id",
			},
			Path: "/",
		}

		user = &userpb.User{
			Id: &userpb.UserId{
				Idp:      "idp",
				OpaqueId: "u-s-e-r-id",
				Type:     userpb.UserType_USER_TYPE_PRIMARY,
			},
			Username: "username",
		}

		firstContent  = []byte("0123456789")
		secondContent = []byte("01234567890123456789")

		ctx = ruser.ContextSetUser(context.Background(), user)

		pub      chan interface{}
		con      chan interface{}
		uploadID string

		fs                   storage.FS
		coordinator          uploadcoordinator.Coordinator
		o                    *options.Options
		lu                   *lookup.Lookup
		pmock                *mocks.PermissionsChecker
		cs3permissionsclient *mocks.CS3PermissionsClient
		permissionsSelector  pool.Selectable[cs3permissions.PermissionsAPIClient]
		bs                   *nodemocks.Blobstore

		succeedPostprocessing = func(uploadID string) {
			// finish postprocessing
			con <- events.PostprocessingFinished{
				UploadID: uploadID,
				Outcome:  events.PPOutcomeContinue,
			}
			// wait for upload to be ready
			ev, ok := (<-pub).(events.UploadReady)
			Expect(ok).To(BeTrue())
			Expect(ev.Failed).To(BeFalse())
			Expect(ev.ResourceID).ToNot(BeNil())
			Expect(ev.ResourceID.OpaqueId).ToNot(BeEmpty())
			Expect(ev.ResourceID.OpaqueId).ToNot(Equal(ev.ResourceID.SpaceId), "ResourceID.OpaqueId should be the file node ID, not the space ID")
		}

		failPostprocessing = func(uploadID string, outcome events.PostprocessingOutcome) {
			// finish postprocessing
			con <- events.PostprocessingFinished{
				UploadID: uploadID,
				Outcome:  outcome,
			}
			// wait for upload to be ready
			ev, ok := (<-pub).(events.UploadReady)
			Expect(ok).To(BeTrue())
			Expect(ev.Failed).To(BeTrue())
		}

		fileStatus = func() (bool, string, int) {
			// check processing status
			resources, err := fs.ListFolder(ctx, rootRef, []string{}, []string{})
			Expect(err).ToNot(HaveOccurred())
			Expect(len(resources)).To(BeElementOf([2]int{0, 1}), "should not have more than one child")

			item := resources[0]
			Expect(item.Path).To(Equal(ref.Path))
			return len(resources) == 1, utils.ReadPlainFromOpaque(item.Opaque, "status"), int(item.GetSize())
		}
		parentSize = func() int {
			parentInfo, err := fs.GetMD(ctx, rootRef, []string{}, []string{})
			Expect(err).ToNot(HaveOccurred())
			return int(parentInfo.Size)
		}
		revisionCount = func() int {
			revisions, err := fs.ListRevisions(ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			return len(revisions)
		}
	)

	BeforeEach(func() {
		// setup test
		tmpRoot, err := helpers.TempDir("reva-unit-tests-*-root")
		Expect(err).ToNot(HaveOccurred())

		o, err = options.New(map[string]interface{}{
			"root":                tmpRoot,
			"treetime_accounting": true,
			"treesize_accounting": true,
			"filemetadatacache": map[string]interface{}{
				"cache_database": tmpRoot,
			},
		})
		Expect(err).ToNot(HaveOccurred())

		lu = lookup.New(metadata.NewMessagePackBackend(o.FileMetadataCache), o, &timemanager.Manager{})
		pmock = &mocks.PermissionsChecker{}

		cs3permissionsclient = &mocks.CS3PermissionsClient{}
		pool.RemoveSelector("PermissionsSelector" + "any")
		permissionsSelector = pool.GetSelector[cs3permissions.PermissionsAPIClient](
			"PermissionsSelector",
			"any",
			func(cc grpc.ClientConnInterface) cs3permissions.PermissionsAPIClient {
				return cs3permissionsclient
			},
		)
		bs = &nodemocks.Blobstore{}

		// create space uses CheckPermission endpoint
		cs3permissionsclient.On("CheckPermission", mock.Anything, mock.Anything, mock.Anything).Return(&cs3permissions.CheckPermissionResponse{
			Status: &v1beta11.Status{Code: v1beta11.Code_CODE_OK},
		}, nil).Times(1)

		p := permissions.NewPermissions(pmock, permissionsSelector)

		// for this test we don't care about permissions
		pmock.On("AssemblePermissions", mock.Anything, mock.Anything).
			Return(&provider.ResourcePermissions{
				Stat:               true,
				GetQuota:           true,
				InitiateFileUpload: true,
				ListContainer:      true,
				ListFileVersions:   true,
			}, nil)

		// setup fs
		pub, con = make(chan interface{}), make(chan interface{})
		tree := tree.New(lu, bs, o, p, store.Create(), &zerolog.Logger{})

		aspects := aspects.Aspects{
			Lookup:      lu,
			Tree:        tree,
			Permissions: p,
			EventStream: stream.Chan{pub, con},
			Trashbin:    &DecomposedfsTrashbin{},
		}
		fs, err = New(o, aspects, &zerolog.Logger{})
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

		bs.On("Delete", mock.Anything).Return(nil).Maybe()
		bs.On("Upload", mock.AnythingOfType("*node.Node"), mock.AnythingOfType("string"), mock.Anything).
			Return(nil).
			Run(func(args mock.Arguments) {
				defer GinkgoRecover()
				n := args.Get(0).(*node.Node)
				data, err := os.ReadFile(args.Get(1).(string))
				Expect(err).ToNot(HaveOccurred())
				Expect(len(data)).To(Equal(int(n.Blobsize)))
			})

		// start upload of a file
		uploadIds, err := coordinator.InitiateUpload(ctx, ref, 10, map[string]string{})
		Expect(err).ToNot(HaveOccurred())
		Expect(len(uploadIds)).To(Equal(2))
		Expect(uploadIds["simple"]).ToNot(BeEmpty())
		Expect(uploadIds["tus"]).ToNot(BeEmpty())

		uploadRef := &provider.Reference{Path: "/" + uploadIds["simple"]}

		_, err = coordinator.Upload(ctx, storage.UploadRequest{
			Ref:    uploadRef,
			Body:   io.NopCloser(bytes.NewReader(firstContent)),
			Length: int64(len(firstContent)),
		}, nil)
		Expect(err).ToNot(HaveOccurred())

		uploadID = uploadIds["simple"]

		// wait for bytes received event
		_, ok := (<-pub).(events.BytesReceived)
		Expect(ok).To(BeTrue())

		// blobstore not called yet
		bs.AssertNumberOfCalls(GinkgoT(), "Upload", 0)
	})

	AfterEach(func() {
		if o.Root != "" {
			os.RemoveAll(o.Root)
		}
		close(pub)
		close(con)
	})

	When("the uploaded file is new", func() {
		It("succeeds eventually", func() {
			// node is created
			resources, err := fs.ListFolder(ctx, rootRef, []string{}, []string{})
			Expect(err).ToNot(HaveOccurred())
			Expect(len(resources)).To(Equal(1))

			item := resources[0]
			Expect(item.Path).To(Equal(ref.Path))
			Expect(utils.ReadPlainFromOpaque(item.Opaque, "status")).To(Equal("processing"))

			succeedPostprocessing(uploadID)

			// blobstore called now
			bs.AssertNumberOfCalls(GinkgoT(), "Upload", 1)

			// node ready
			resources, err = fs.ListFolder(ctx, rootRef, []string{}, []string{})
			Expect(err).ToNot(HaveOccurred())
			Expect(len(resources)).To(Equal(1))

			item = resources[0]
			Expect(item.Path).To(Equal(ref.Path))
			Expect(utils.ReadPlainFromOpaque(item.Opaque, "status")).To(BeEmpty())

		})

		It("deletes node and bytes when instructed", func() {
			// node is created
			resources, err := fs.ListFolder(ctx, rootRef, []string{}, []string{})
			Expect(err).ToNot(HaveOccurred())
			Expect(len(resources)).To(Equal(1))

			item := resources[0]
			Expect(item.Path).To(Equal(ref.Path))
			Expect(utils.ReadPlainFromOpaque(item.Opaque, "status")).To(Equal("processing"))

			// bytes are in dedicated path
			_, err = os.Stat(filepath.Join(o.Root, "uploads", uploadID))
			Expect(err).To(BeNil())

			failPostprocessing(uploadID, events.PPOutcomeDelete)

			// blobstore still not called now
			bs.AssertNumberOfCalls(GinkgoT(), "Upload", 0)

			// node gone
			resources, err = fs.ListFolder(ctx, rootRef, []string{}, []string{})
			Expect(err).ToNot(HaveOccurred())
			Expect(len(resources)).To(Equal(0))

			// bytes gone
			_, err = os.Stat(filepath.Join(o.Root, "uploads", uploadID))
			Expect(err).ToNot(BeNil())
		})

		// orphanNode simulates an orphaned node: the node file is still there but
		// its metadata is gone, e.g. because an ancestor was trashed while the
		// upload was in flight. Reading the node then fails. It purges instead of
		// removing the metadata file directly, so the cached attributes go as well.
		orphanNode := func() string {
			// node is created and the optimistic size has been propagated
			resources, err := fs.ListFolder(ctx, rootRef, []string{}, []string{})
			Expect(err).ToNot(HaveOccurred())
			Expect(len(resources)).To(Equal(1))
			Expect(parentSize()).To(Equal(len(firstContent)))

			spaceID, nodeID := ref.GetResourceId().GetSpaceId(), resources[0].GetId().GetOpaqueId()
			nodePath := lu.InternalPath(spaceID, nodeID)
			n, err := node.ReadNode(ctx, lu, spaceID, nodeID, "", false, nil, true)
			Expect(err).ToNot(HaveOccurred())
			Expect(lu.MetadataBackend().Purge(ctx, n)).To(Succeed())
			_, err = node.ReadNode(ctx, lu, spaceID, nodeID, "", false, nil, true)
			Expect(err).To(MatchError(node.ErrMissingParentID), "node should be orphaned after purging its metadata")
			return nodePath
		}
		sessionSizeDiff := func() (string, bool) {
			data, err := os.ReadFile(filepath.Join(o.Root, "uploads", uploadID+".info"))
			Expect(err).ToNot(HaveOccurred())
			info := struct{ MetaData map[string]string }{}
			Expect(json.Unmarshal(data, &info)).To(Succeed())
			sizeDiff, ok := info.MetaData["sizeDiff"]
			return sizeDiff, ok
		}

		It("releases the quota and removes the node and bytes of an orphaned upload when instructed", func() {
			nodePath := orphanNode()

			failPostprocessing(uploadID, events.PPOutcomeDelete)

			_, err := os.Stat(filepath.Join(o.Root, "uploads", uploadID))
			Expect(err).To(HaveOccurred(), "the upload bytes should be cleaned up")
			_, err = os.Stat(filepath.Join(o.Root, "uploads", uploadID+".info"))
			Expect(err).To(HaveOccurred(), "the upload info should be cleaned up")

			// the blob was never written
			bs.AssertNumberOfCalls(GinkgoT(), "Upload", 0)

			// the orphaned node is gone ...
			_, err = os.Stat(nodePath)
			Expect(err).ToNot(BeNil())

			// ... and most importantly the quota has been released
			Expect(parentSize()).To(Equal(0))
		})

		It("releases the quota of an orphaned upload that cannot be committed once and keeps its bytes", func() {
			nodePath := orphanNode()
			sizeDiff, ok := sessionSizeDiff()
			Expect(ok).To(BeTrue())
			Expect(sizeDiff).To(Equal("10"))

			// the commit cannot resolve the node, so the upload is rolled back
			failPostprocessing(uploadID, events.PPOutcomeContinue)

			_, err := os.Stat(nodePath)
			Expect(err).To(HaveOccurred(), "the orphaned node should be removed")
			Expect(parentSize()).To(Equal(0))

			// the size diff has been cleared, so a repeated cleanup does not
			// release the quota a second time
			sizeDiff, _ = sessionSizeDiff()
			Expect(sizeDiff).To(Equal("0"))

			// the bytes and the info are kept for a restart
			_, err = os.Stat(filepath.Join(o.Root, "uploads", uploadID))
			Expect(err).ToNot(HaveOccurred())
			_, err = os.Stat(filepath.Join(o.Root, "uploads", uploadID+".info"))
			Expect(err).ToNot(HaveOccurred())
			bs.AssertNumberOfCalls(GinkgoT(), "Upload", 0)
		})

		It("deletes node and keeps the bytes when instructed", func() {
			// node is created
			resources, err := fs.ListFolder(ctx, rootRef, []string{}, []string{})
			Expect(err).ToNot(HaveOccurred())
			Expect(len(resources)).To(Equal(1))

			item := resources[0]
			Expect(item.Path).To(Equal(ref.Path))
			Expect(utils.ReadPlainFromOpaque(item.Opaque, "status")).To(Equal("processing"))

			// bytes are in dedicated path
			_, err = os.Stat(filepath.Join(o.Root, "uploads", uploadID))
			Expect(err).To(BeNil())

			failPostprocessing(uploadID, events.PPOutcomeAbort)

			// blobstore still not called now
			bs.AssertNumberOfCalls(GinkgoT(), "Upload", 0)

			// node gone
			resources, err = fs.ListFolder(ctx, rootRef, []string{}, []string{})
			Expect(err).ToNot(HaveOccurred())
			Expect(len(resources)).To(Equal(0))

			// bytes are still here
			_, err = os.Stat(filepath.Join(o.Root, "uploads", uploadID))
			Expect(err).To(BeNil())
		})
	})

	When("the uploaded file creates a new version", func() {
		JustBeforeEach(func() {
			succeedPostprocessing(uploadID)

			// make sure there is no version yet
			revs, err := fs.ListRevisions(ctx, ref)
			Expect(err).To(BeNil())
			Expect(len(revs)).To(Equal(0))

			// upload again
			uploadIds, err := coordinator.InitiateUpload(ctx, ref, 10, map[string]string{})
			Expect(err).ToNot(HaveOccurred())
			Expect(len(uploadIds)).To(Equal(2))
			Expect(uploadIds["simple"]).ToNot(BeEmpty())
			Expect(uploadIds["tus"]).ToNot(BeEmpty())

			uploadRef := &provider.Reference{Path: "/" + uploadIds["simple"]}

			_, err = coordinator.Upload(ctx, storage.UploadRequest{
				Ref:    uploadRef,
				Body:   io.NopCloser(bytes.NewReader(firstContent)),
				Length: int64(len(firstContent)),
			}, nil)
			Expect(err).ToNot(HaveOccurred())

			uploadID = uploadIds["simple"]

			// wait for bytes received event
			_, ok := (<-pub).(events.BytesReceived)
			Expect(ok).To(BeTrue())

			// version already created
			revs, err = fs.ListRevisions(ctx, ref)
			Expect(err).To(BeNil())
			Expect(len(revs)).To(Equal(1))

			// at this stage: blobstore called once for the original file
			bs.AssertNumberOfCalls(GinkgoT(), "Upload", 1)

		})

		It("succeeds eventually, creating a new version", func() {
			succeedPostprocessing(uploadID)

			// version still existing
			revs, err := fs.ListRevisions(ctx, ref)
			Expect(err).To(BeNil())
			Expect(len(revs)).To(Equal(1))

			// blobstore now called twice - for original file and new version
			bs.AssertNumberOfCalls(GinkgoT(), "Upload", 2)

			// bytes are gone from upload path
			_, err = os.Stat(filepath.Join(o.Root, "uploads", uploadID))
			Expect(err).ToNot(BeNil())
		})

		It("removes new version and restores old one when instructed", func() {
			_, status, _ := fileStatus()
			Expect(status).To(Equal("processing"))

			failPostprocessing(uploadID, events.PPOutcomeDelete)

			_, status, _ = fileStatus()
			Expect(status).To(Equal(""))

			// version gone now
			revs, err := fs.ListRevisions(ctx, ref)
			Expect(err).To(BeNil())
			Expect(len(revs)).To(Equal(0))

			// bytes are removed from upload path
			_, err = os.Stat(filepath.Join(o.Root, "uploads", uploadID))
			Expect(err).ToNot(BeNil())

			// blobstore still called only once for the original file
			bs.AssertNumberOfCalls(GinkgoT(), "Upload", 1)
		})

	})
	When("Two uploads are processed in parallel", func() {
		var secondUploadID string

		JustBeforeEach(func() {
			// upload again
			uploadIds, err := coordinator.InitiateUpload(ctx, ref, 20, map[string]string{})
			Expect(err).ToNot(HaveOccurred())
			Expect(len(uploadIds)).To(Equal(2))
			Expect(uploadIds["simple"]).ToNot(BeEmpty())
			Expect(uploadIds["tus"]).ToNot(BeEmpty())

			uploadRef := &provider.Reference{Path: "/" + uploadIds["simple"]}

			_, err = coordinator.Upload(ctx, storage.UploadRequest{
				Ref:    uploadRef,
				Body:   io.NopCloser(bytes.NewReader(secondContent)),
				Length: int64(len(secondContent)),
			}, nil)
			Expect(err).ToNot(HaveOccurred())

			secondUploadID = uploadIds["simple"]

			// wait for bytes received event
			_, ok := (<-pub).(events.BytesReceived)
			Expect(ok).To(BeTrue())
		})

		It("doesn't remove processing status when first upload is finished", func() {
			succeedPostprocessing(uploadID)

			_, status, _ := fileStatus()
			// check processing status
			Expect(status).To(Equal("processing"))
		})

		It("removes processing status when second upload is finished, even if first isn't", func() {
			succeedPostprocessing(secondUploadID)

			_, status, _ := fileStatus()
			Expect(status).To(Equal(""))
		})

		It("correctly calculates the size when the second upload is finished, even if first is deleted", func() {
			succeedPostprocessing(secondUploadID)

			_, status, size := fileStatus()
			Expect(status).To(Equal(""))
			// size should match the second upload
			Expect(size).To(Equal(len(secondContent)))

			// parent size should match second upload as well
			Expect(parentSize()).To(Equal(len(secondContent)))

			failPostprocessing(uploadID, events.PPOutcomeDelete)

			// check processing status
			_, _, size = fileStatus()
			// size should still match the second upload
			Expect(size).To(Equal(len(secondContent)))

			// parent size should still match second upload as well
			Expect(parentSize()).To(Equal(len(secondContent)))
		})

		It("the first can succeed before the second succeeds", func() {
			succeedPostprocessing(uploadID)

			_, status, size := fileStatus()
			// check processing status
			Expect(status).To(Equal("processing"))
			// size should match the second upload
			Expect(size).To(Equal((len(secondContent))))

			// parent size should match the second upload
			Expect(parentSize()).To(Equal(len(secondContent)))

			succeedPostprocessing(secondUploadID)

			// check processing status has been removed
			_, status, size = fileStatus()
			Expect(status).To(Equal(""))

			// size should still match the second upload
			Expect(size).To(Equal(len(secondContent)))

			// parent size should still match second upload
			Expect(parentSize()).To(Equal(len(secondContent)))

			// file should have one revision
			Expect(revisionCount()).To(Equal(1))
		})

		It("the first can succeed after the second succeeds", func() {
			succeedPostprocessing(secondUploadID)

			_, status, size := fileStatus()
			// check processing status has been removed because the most recent upload finished and can be downloaded
			Expect(status).To(Equal(""))
			// size should match the second upload
			Expect(size).To(Equal(len(secondContent)))

			// parent size should match second upload as well
			Expect(parentSize()).To(Equal(len(secondContent)))

			succeedPostprocessing(uploadID)

			_, status, size = fileStatus()
			// check processing status is still unset
			Expect(status).To(Equal(""))
			// size should still match the second upload
			Expect(size).To(Equal(len(secondContent)))

			// parent size should still match second upload
			Expect(parentSize()).To(Equal(len(secondContent)))

			// file should have one revision
			Expect(revisionCount()).To(Equal(1))
		})

		It("the first can succeed before the second fails", func() {
			succeedPostprocessing(uploadID)

			_, status, size := fileStatus()
			// check processing status
			Expect(status).To(Equal("processing"))
			// size should match the second upload
			Expect(size).To(Equal(len(secondContent)))

			// parent size should match the second upload
			Expect(parentSize()).To(Equal(len(secondContent)))

			failPostprocessing(secondUploadID, events.PPOutcomeDelete)

			_, status, size = fileStatus()
			// check processing status has been removed
			Expect(status).To(Equal(""))
			// size should match the first upload
			Expect(size).To(Equal(len(firstContent)))

			// parent size should match first upload
			Expect(parentSize()).To(Equal(len(firstContent)))

			// file should not have any revisions
			Expect(revisionCount()).To(Equal(0))
		})

		It("the first can succeed after the second fails", func() {
			failPostprocessing(secondUploadID, events.PPOutcomeDelete)

			_, _, size := fileStatus()
			// check processing status has not been unset
			// FIXME we need to fall back to the previous processing id
			// Expect(status).To(Equal("processing"))
			// size should match the first upload
			Expect(size).To(Equal(len(firstContent)))

			// parent size should match first upload as well
			Expect(parentSize()).To(Equal(len(firstContent)))

			succeedPostprocessing(uploadID)

			_, status, size := fileStatus()
			// check processing status is now unset
			Expect(status).To(Equal(""))
			// size should still match the first upload
			Expect(size).To(Equal(len(firstContent)))

			// parent size should still match first upload
			Expect(parentSize()).To(Equal(len(firstContent)))

			// file should not have any revisions
			Expect(revisionCount()).To(Equal(0))
		})

		It("the first can fail before the second succeeds", func() {
			failPostprocessing(uploadID, events.PPOutcomeDelete)

			_, status, size := fileStatus()
			// check processing status
			Expect(status).To(Equal("processing"))
			// size should match the second upload
			Expect(size).To(Equal(len(secondContent)))

			// parent size should match second upload as well
			Expect(parentSize()).To(Equal(len(secondContent)))

			succeedPostprocessing(secondUploadID)

			_, status, size = fileStatus()
			// check processing status has been removed
			Expect(status).To(Equal(""))
			// size should still match the second upload
			Expect(size).To(Equal(len(secondContent)))

			// parent size should still match second upload
			Expect(parentSize()).To(Equal(len(secondContent)))

			// file should not have any revisions
			// FIXME we need to delete the revision
			// Expect(revisionCount()).To(Equal(0))
		})

		It("the first can fail after the second succeeds", func() {
			succeedPostprocessing(secondUploadID)

			_, status, size := fileStatus()
			// check processing status has been removed because the most recent upload finished and can be downloaded
			Expect(status).To(Equal(""))
			// size should match the second upload
			Expect(size).To(Equal(len(secondContent)))

			// parent size should match second upload as well
			Expect(parentSize()).To(Equal(len(secondContent)))

			failPostprocessing(uploadID, events.PPOutcomeDelete)

			_, status, size = fileStatus()
			// check processing status is still unset
			Expect(status).To(Equal(""))
			// size should still match the second upload
			Expect(size).To(Equal(len(secondContent)))

			// parent size should still match second upload
			Expect(parentSize()).To(Equal(len(secondContent)))

			// file should not have any revisions
			// FIXME we need to delete the revision
			// Expect(revisionCount()).To(Equal(0))
		})

		It("the first can fail before the second fails", func() {
			failPostprocessing(uploadID, events.PPOutcomeDelete)

			_, status, size := fileStatus()
			// check processing status
			Expect(status).To(Equal("processing"))
			// size should match the second upload
			Expect(size).To(Equal(len(secondContent)))

			// parent size should match second upload as well
			Expect(parentSize()).To(Equal(len(secondContent)))

			failPostprocessing(secondUploadID, events.PPOutcomeDelete)

			// the first upload created the file and is gone, so the file is removed
			resources, err := fs.ListFolder(ctx, rootRef, []string{}, []string{})
			Expect(err).ToNot(HaveOccurred())
			Expect(resources).To(BeEmpty())
			Expect(parentSize()).To(Equal(0))
		})

		It("the first can fail after the second fails", func() {
			failPostprocessing(secondUploadID, events.PPOutcomeDelete)

			_, status, size := fileStatus()
			// the file is handed back to the first upload, which is still processing
			Expect(status).To(Equal("processing"))
			// size should match the first upload
			Expect(size).To(Equal(len(firstContent)))

			// parent size should match second first as well
			Expect(parentSize()).To(Equal(len(firstContent)))

			failPostprocessing(uploadID, events.PPOutcomeDelete)

			// the first upload created the file, so failing it removes the file
			resources, err := fs.ListFolder(ctx, rootRef, []string{}, []string{})
			Expect(err).ToNot(HaveOccurred())
			Expect(resources).To(BeEmpty())
			Expect(parentSize()).To(Equal(0))
		})
	})
})
