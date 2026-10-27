// Copyright 2018-2021 CERN
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// In applying this license, CERN does not waive the privileges and immunities
// granted to it by virtue of its status as an Intergovernmental Organization
// or submit itself to any jurisdiction.

package decomposedfs_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	cs3permissions "github.com/cs3org/go-cs3apis/cs3/permissions/v1beta1"
	v1beta11 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	ruser "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs"
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
	"github.com/opencloud-eu/reva/v2/pkg/upload"
	"github.com/opencloud-eu/reva/v2/tests/helpers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
	tusd "github.com/tus/tusd/v2/pkg/handler"
	"google.golang.org/grpc"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("File uploads", func() {
	var (
		ref     *provider.Reference
		rootRef *provider.Reference
		fs      storage.FS
		coord   upload.Coordinator
		user    *userpb.User
		ctx     context.Context

		o                    *options.Options
		lu                   *lookup.Lookup
		pmock                *mocks.PermissionsChecker
		cs3permissionsclient *mocks.CS3PermissionsClient
		permissionsSelector  pool.Selectable[cs3permissions.PermissionsAPIClient]
		bs                   *nodemocks.Blobstore
	)

	BeforeEach(func() {
		ref = &provider.Reference{
			ResourceId: &provider.ResourceId{
				SpaceId: "u-s-e-r-id",
			},
			Path: "/foo",
		}

		user = &userpb.User{
			Id: &userpb.UserId{
				Idp:      "idp",
				OpaqueId: "u-s-e-r-id",
				Type:     userpb.UserType_USER_TYPE_PRIMARY,
			},
			Username: "username",
		}

		rootRef = &provider.Reference{
			ResourceId: &provider.ResourceId{
				SpaceId:  "u-s-e-r-id",
				OpaqueId: "u-s-e-r-id",
			},
			Path: "/",
		}

		ctx = ruser.ContextSetUser(context.Background(), user)

		tmpRoot, err := helpers.TempDir("reva-unit-tests-*-root")
		Expect(err).ToNot(HaveOccurred())

		o, err = options.New(map[string]interface{}{
			"root": tmpRoot,
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
	})

	AfterEach(func() {
		root := o.Root
		if root != "" {
			os.RemoveAll(root)
		}
	})

	BeforeEach(func() {
		cs3permissionsclient.On("CheckPermission", mock.Anything, mock.Anything, mock.Anything).Return(&cs3permissions.CheckPermissionResponse{
			Status: &v1beta11.Status{Code: v1beta11.Code_CODE_OK},
		}, nil)
		pmock.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
			Stat:     true,
			AddGrant: true,
		}, nil).Times(1)
		var err error
		tree := tree.New(lu, bs, o, permissions.Permissions{}, store.Create(), &zerolog.Logger{})

		aspects := aspects.Aspects{
			Lookup:      lu,
			Tree:        tree,
			Permissions: permissions.NewPermissions(pmock, permissionsSelector),
			Trashbin:    &decomposedfs.DecomposedfsTrashbin{},
		}
		fs, err = decomposedfs.New(o, aspects, &zerolog.Logger{})
		Expect(err).ToNot(HaveOccurred())

		resp, err := fs.CreateStorageSpace(ctx, &provider.CreateStorageSpaceRequest{Owner: user, Type: "personal"})
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Status.Code).To(Equal(v1beta11.Code_CODE_OK))
		resID, err := storagespace.ParseID(resp.StorageSpace.Id.OpaqueId)
		Expect(err).ToNot(HaveOccurred())
		ref.ResourceId = &resID

		// The coordinator owns the upload flow; the driver only sees the slim contract.
		// No publisher and no chunking: without a postprocessing consumer an upload
		// finishes synchronously, so the specs below observe the driver's final state.
		log := zerolog.Nop()
		coord, err = upload.NewCoordinatorFromConfig(GinkgoT().TempDir(), nil, fs, nil, &log, false)
		Expect(err).ToNot(HaveOccurred())
	})

	// The quota and permission checks at initiate now live in the coordinator, which
	// resolves them through GetQuota/GetMD rather than node.CheckQuota. They are
	// asserted against the coordinator directly in pkg/upload/initiate_test.go.

	Context("with sufficient permissions", func() {
		BeforeEach(func() {
			pmock.On("AssemblePermissions", mock.Anything, mock.Anything).
				Return(&provider.ResourcePermissions{
					Stat:               true,
					GetQuota:           true,
					InitiateFileUpload: true,
					ListContainer:      true,
				}, nil)
		})

		When("the user initiates a non zero byte file upload", func() {
			It("succeeds", func() {
				uploadIds, err := coord.InitiateUpload(ctx, ref, 10, map[string]string{})

				Expect(err).ToNot(HaveOccurred())
				Expect(len(uploadIds)).To(Equal(2))
				Expect(uploadIds["simple"]).ToNot(BeEmpty())
				Expect(uploadIds["tus"]).ToNot(BeEmpty())

				resources, err := fs.ListFolder(ctx, rootRef, []string{}, []string{})
				Expect(err).ToNot(HaveOccurred())
				Expect(len(resources)).To(Equal(0))
			})
		})

		When("the user initiates a zero byte file upload", func() {
			It("succeeds", func() {
				bs.On("Upload", mock.AnythingOfType("*node.Node"), mock.AnythingOfType("string"), mock.Anything).
					Return(nil)
				uploadIds, err := coord.InitiateUpload(ctx, ref, 0, map[string]string{})

				Expect(err).ToNot(HaveOccurred())
				Expect(len(uploadIds)).To(Equal(2))
				Expect(uploadIds["simple"]).ToNot(BeEmpty())
				Expect(uploadIds["tus"]).ToNot(BeEmpty())

				resources, err := fs.ListFolder(ctx, rootRef, []string{}, []string{})
				Expect(err).ToNot(HaveOccurred())
				Expect(len(resources)).To(Equal(1))
			})

			It("fails when trying to upload empty data. 0-byte uploads are finished during initialization already", func() {
				bs.On("Upload", mock.AnythingOfType("*node.Node"), mock.AnythingOfType("string"), mock.Anything).
					Return(nil)
				uploadIds, err := coord.InitiateUpload(ctx, ref, 0, map[string]string{})

				Expect(err).ToNot(HaveOccurred())
				Expect(len(uploadIds)).To(Equal(2))
				Expect(uploadIds["simple"]).ToNot(BeEmpty())

				uploadRef := &provider.Reference{Path: "/" + uploadIds["simple"]}

				_, err = coord.Upload(ctx, upload.Request{
					Ref:    uploadRef,
					Body:   io.NopCloser(bytes.NewReader([]byte(""))),
					Length: 0,
				}, nil)

				Expect(err).To(HaveOccurred())
			})
		})

		When("the user uploads a non zero byte file", func() {
			It("succeeds", func() {
				var (
					fileContent = []byte("0123456789")
				)

				uploadIds, err := coord.InitiateUpload(ctx, ref, 10, map[string]string{})

				Expect(err).ToNot(HaveOccurred())
				Expect(len(uploadIds)).To(Equal(2))
				Expect(uploadIds["simple"]).ToNot(BeEmpty())
				Expect(uploadIds["tus"]).ToNot(BeEmpty())

				uploadRef := &provider.Reference{Path: "/" + uploadIds["simple"]}

				bs.On("Upload", mock.AnythingOfType("*node.Node"), mock.AnythingOfType("string"), mock.Anything).
					Return(nil).
					Run(func(args mock.Arguments) {
						data, err := os.ReadFile(args.Get(1).(string))

						Expect(err).ToNot(HaveOccurred())
						Expect(data).To(Equal([]byte("0123456789")))
					})

				_, err = coord.Upload(ctx, upload.Request{
					Ref:    uploadRef,
					Body:   io.NopCloser(bytes.NewReader(fileContent)),
					Length: int64(len(fileContent)),
				}, nil)

				Expect(err).ToNot(HaveOccurred())
				bs.AssertCalled(GinkgoT(), "Upload", mock.Anything, mock.Anything, mock.Anything)

				resources, err := fs.ListFolder(ctx, rootRef, []string{}, []string{})

				Expect(err).ToNot(HaveOccurred())
				Expect(len(resources)).To(Equal(1))
				Expect(resources[0].Path).To(Equal(ref.Path))
			})
		})

		When("the quota is exceeded when the upload is finished", func() {
			It("fails with insufficient storage instead of an internal server error", func() {
				var (
					fileContent = []byte("0123456789")
				)

				uploadIds, err := coord.InitiateUpload(ctx, ref, 10, map[string]string{})
				Expect(err).ToNot(HaveOccurred())

				session, err := coord.GetUpload(ctx, uploadIds["tus"])
				Expect(err).ToNot(HaveOccurred())

				_, err = session.WriteChunk(ctx, 0, bytes.NewReader(fileContent))
				Expect(err).ToNot(HaveOccurred())

				// the quota is only exceeded once the upload is finished, e.g. because
				// another upload into the same space filled it up in the meantime
				originalFunc := node.CheckQuota
				node.CheckQuota = func(ctx context.Context, spaceRoot *node.Node, overwrite bool, oldSize, newSize uint64) (bool, error) {
					return false, errtypes.InsufficientStorage("quota exceeded")
				}
				defer func() { node.CheckQuota = originalFunc }()

				err = session.FinishUpload(ctx)

				// tusd only maps its own errors to a status code, everything else
				// becomes an internal server error
				var tusErr tusd.Error
				Expect(errors.As(err, &tusErr)).To(BeTrue())
				Expect(tusErr.ErrorCode).To(Equal("ERR_INSUFFICIENT_STORAGE"))
				Expect(tusErr.HTTPResponse.StatusCode).To(Equal(http.StatusInsufficientStorage))
			})
		})

		When("the user tries to upload a file without intialising the upload", func() {
			It("fails", func() {
				var (
					fileContent = []byte("0123456789")
				)

				uploadRef := &provider.Reference{Path: "/some-non-existent-upload-reference"}
				_, err := coord.Upload(ctx, upload.Request{
					Ref:    uploadRef,
					Body:   io.NopCloser(bytes.NewReader(fileContent)),
					Length: int64(len(fileContent)),
				}, nil)

				Expect(err).To(HaveOccurred())

				resources, err := fs.ListFolder(ctx, rootRef, []string{}, []string{})

				Expect(err).ToNot(HaveOccurred())
				Expect(len(resources)).To(Equal(0))
			})
		})

		Describe("CommitUpload", func() {
			body := []byte("staged upload")

			prepare := func(sessionID string, nodeExisted bool) {
				_, err := fs.PrepareUpload(ctx, ref, sessionID, storage.UploadInfo{
					NodeExisted: nodeExisted,
					Size:        int64(len(body)),
				})
				Expect(err).ToNot(HaveOccurred())
			}

			BeforeEach(func() {
				Expect(fs.TouchFile(ctx, ref, false, "")).To(Succeed())
			})

			It("writes the staged bytes to the node prepared for the session", func() {
				prepare("session-1", false)

				bs.On("UploadFromReader", mock.AnythingOfType("*node.Node"), mock.Anything, int64(len(body))).
					Run(func(args mock.Arguments) {
						n := args.Get(0).(*node.Node)
						Expect(n.BlobID).To(Equal("session-1"))
						Expect(n.SpaceRoot).ToNot(BeNil())
						data, err := io.ReadAll(args.Get(1).(io.Reader))
						Expect(err).ToNot(HaveOccurred())
						Expect(data).To(Equal(body))
					}).Return(nil).Once()

				Expect(fs.CommitUpload(ctx, ref, "session-1", storage.UploadSource{
					Body:   io.NopCloser(bytes.NewReader(body)),
					Length: int64(len(body)),
				})).To(Succeed())
				bs.AssertExpectations(GinkgoT())
			})

			It("moves a staged file into place", func() {
				prepare("session-1", false)

				bs.On("Upload", mock.AnythingOfType("*node.Node"), "/staged/session-1", mock.Anything).
					Run(func(args mock.Arguments) {
						Expect(args.Get(0).(*node.Node).BlobID).To(Equal("session-1"))
					}).Return(nil).Once()

				Expect(fs.CommitUpload(ctx, ref, "session-1", storage.UploadSource{
					Path:   "/staged/session-1",
					Length: int64(len(body)),
				})).To(Succeed())
				bs.AssertExpectations(GinkgoT())
			})

			It("writes into the revision when a newer upload took over the node", func() {
				prepare("session-a", false)
				prepare("session-b", true)

				bs.On("UploadFromReader", mock.AnythingOfType("*node.Node"), mock.Anything, int64(len(body))).
					Run(func(args mock.Arguments) {
						n := args.Get(0).(*node.Node)
						Expect(n.BlobID).To(Equal("session-a"))
						Expect(n.ID).To(ContainSubstring(node.RevisionIDDelimiter))
					}).Return(nil).Once()

				Expect(fs.CommitUpload(ctx, ref, "session-a", storage.UploadSource{
					Body:   io.NopCloser(bytes.NewReader(body)),
					Length: int64(len(body)),
				})).To(Succeed())
				bs.AssertExpectations(GinkgoT())
			})

			It("records the scan result on the node", func() {
				prepare("session-1", false)
				bs.On("UploadFromReader", mock.AnythingOfType("*node.Node"), mock.Anything, int64(len(body))).Return(nil).Once()

				scanDate := time.Now().UTC().Truncate(time.Second)
				Expect(fs.CommitUpload(ctx, ref, "session-1", storage.UploadSource{
					Body:       io.NopCloser(bytes.NewReader(body)),
					Length:     int64(len(body)),
					ScanResult: "infected",
					ScanDate:   scanDate,
				})).To(Succeed())

				n, err := lu.NodeFromResource(ctx, ref)
				Expect(err).ToNot(HaveOccurred())
				scanned, virus, date := n.ScanData(ctx)
				Expect(scanned).To(BeTrue())
				Expect(virus).To(Equal("infected"))
				Expect(date).To(BeTemporally("==", scanDate))
			})

			It("records the scan result on the revision when a newer upload took over the node", func() {
				prepare("session-a", false)
				prepare("session-b", true)

				var revision *node.Node
				bs.On("UploadFromReader", mock.AnythingOfType("*node.Node"), mock.Anything, int64(len(body))).
					Run(func(args mock.Arguments) {
						revision = args.Get(0).(*node.Node)
					}).Return(nil).Once()

				Expect(fs.CommitUpload(ctx, ref, "session-a", storage.UploadSource{
					Body:       io.NopCloser(bytes.NewReader(body)),
					Length:     int64(len(body)),
					ScanResult: "infected",
					ScanDate:   time.Now(),
				})).To(Succeed())

				Expect(revision).ToNot(BeNil())
				_, virus, _ := revision.ScanData(ctx)
				Expect(virus).To(Equal("infected"))

				n, err := lu.NodeFromResource(ctx, ref)
				Expect(err).ToNot(HaveOccurred())
				scanned, _, _ := n.ScanData(ctx)
				Expect(scanned).To(BeFalse(), "the scan result of the older upload must not land on the node")
			})

			It("rejects a source with neither body nor path", func() {
				err := fs.CommitUpload(ctx, ref, "session-1", storage.UploadSource{Length: 1})

				Expect(err).To(HaveOccurred())
				_, ok := err.(errtypes.IsBadRequest)
				Expect(ok).To(BeTrue(), "expected errtypes.BadRequest, got %T: %v", err, err)
			})
		})

		It("rejects an empty sessionID", func() {
			err := fs.CommitUpload(ctx, ref, "", storage.UploadSource{
				Body:   io.NopCloser(bytes.NewReader([]byte("x"))),
				Length: 1,
			})

			Expect(err).To(HaveOccurred())
			_, ok := err.(errtypes.IsBadRequest)
			Expect(ok).To(BeTrue(), "expected errtypes.BadRequest, got %T: %v", err, err)
		})

		It("only clears a processing marker for its owning session", func() {
			pmock.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
				Stat:               true,
				InitiateFileUpload: true,
			}, nil).Once()
			Expect(fs.TouchFile(ctx, ref, false, "")).To(Succeed())

			Expect(fs.MarkProcessing(ctx, ref, true, "session-1")).To(Succeed())
			Expect(fs.MarkProcessing(ctx, ref, false, "session-2")).To(Succeed())
			file, err := lu.NodeFromResource(ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			Expect(file.IsProcessing(ctx)).To(BeTrue())

			Expect(fs.MarkProcessing(ctx, ref, false, "session-1")).To(Succeed())
			file, err = lu.NodeFromResource(ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			Expect(file.IsProcessing(ctx)).To(BeFalse())
		})

		It("lets a newer session take over the processing marker", func() {
			pmock.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
				Stat:               true,
				InitiateFileUpload: true,
			}, nil).Once()
			Expect(fs.TouchFile(ctx, ref, false, "")).To(Succeed())

			Expect(fs.MarkProcessing(ctx, ref, true, "session-1")).To(Succeed())
			Expect(fs.MarkProcessing(ctx, ref, true, "session-2")).To(Succeed())

			file, err := lu.NodeFromResource(ctx, ref)
			Expect(err).ToNot(HaveOccurred())
			id, err := file.ProcessingID(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal("session-2"))
		})

	})
})
