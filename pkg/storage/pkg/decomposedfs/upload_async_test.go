package decomposedfs_test

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"hash/adler32"
	"io"
	"os"
	"path/filepath"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	cs3permissions "github.com/cs3org/go-cs3apis/cs3/permissions/v1beta1"
	v1beta11 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	ruser "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/events"
	"github.com/opencloud-eu/reva/v2/pkg/events/stream"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/idcache"
	posixoptions "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/options"
	posixhelpers "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/testhelpers"
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
	"github.com/opencloud-eu/reva/v2/pkg/utils"
	"github.com/opencloud-eu/reva/v2/tests/helpers"
	"github.com/pkg/xattr"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func defineAsyncUploadTests(name string, usePosix bool) {
	decorators := []any{Ordered}
	if usePosix {
		decorators = append(decorators, ContinueOnFailure)
	}
	decorators = append(decorators, func() {
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
			o                    *options.Options
			lu                   *lookup.Lookup
			pmock                *mocks.PermissionsChecker
			cs3permissionsclient *mocks.CS3PermissionsClient
			permissionsSelector  pool.Selectable[cs3permissions.PermissionsAPIClient]
			bs                   *nodemocks.Blobstore
			blobs                map[string][]byte
			natsConn             *nats.Conn
			js                   jetstream.JetStream
			cleanupNATS          func()

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
			fileAttributesMatchContents = func(downloadRef *provider.Reference) {
				md, reader, err := fs.Download(ctx, downloadRef, func(*provider.ResourceInfo) bool { return true })
				Expect(err).ToNot(HaveOccurred())
				contents, err := io.ReadAll(reader)
				Expect(err).ToNot(HaveOccurred())
				Expect(reader.Close()).To(Succeed())

				sha1Sum := sha1.Sum(contents)
				md5Sum := md5.Sum(contents)
				adler32Sum := adler32.New()
				_, err = adler32Sum.Write(contents)
				Expect(err).ToNot(HaveOccurred())

				Expect(md.Size).To(Equal(uint64(len(contents))))
				Expect(md.GetChecksum().GetSum()).To(Equal(hex.EncodeToString(sha1Sum[:])))
				Expect(utils.ReadPlainFromOpaque(md.Opaque, "md5")).To(Equal(hex.EncodeToString(md5Sum[:])))
				Expect(utils.ReadPlainFromOpaque(md.Opaque, "adler32")).To(Equal(hex.EncodeToString(adler32Sum.Sum(nil))))
			}
			fileAndRevisionAttributesMatchContents = func() {
				fileAttributesMatchContents(ref)

				revisions, err := fs.ListRevisions(ctx, ref)
				Expect(err).ToNot(HaveOccurred())
				Expect(revisions).To(HaveLen(1))
				fileAttributesMatchContents(&provider.Reference{ResourceId: &provider.ResourceId{
					StorageId: ref.ResourceId.StorageId,
					SpaceId:   ref.ResourceId.SpaceId,
					OpaqueId:  revisions[0].Key,
				}})
			}
		)

		BeforeAll(func() {
			if !usePosix {
				return
			}
			var err error
			natsConn, js, cleanupNATS, err = posixhelpers.NewInProcessNATSServer()
			Expect(err).ToNot(HaveOccurred())
		})

		AfterAll(func() {
			if !usePosix {
				return
			}
			natsConn.Close()
			cleanupNATS()
		})

		BeforeEach(func() {
			// setup test
			tmpRoot, err := helpers.TempDir("reva-unit-tests-*-root")
			Expect(err).ToNot(HaveOccurred())

			config := map[string]interface{}{
				"root":                tmpRoot,
				"asyncfileuploads":    true,
				"treetime_accounting": true,
				"treesize_accounting": true,
				"filemetadatacache": map[string]interface{}{
					"cache_database": tmpRoot,
				},
			}
			o, err = options.New(config)
			Expect(err).ToNot(HaveOccurred())

			cs3permissionsclient = &mocks.CS3PermissionsClient{}
			pool.RemoveSelector("PermissionsSelector" + "any")
			permissionsSelector = pool.GetSelector[cs3permissions.PermissionsAPIClient](
				"PermissionsSelector",
				"any",
				func(cc grpc.ClientConnInterface) cs3permissions.PermissionsAPIClient {
					return cs3permissionsclient
				},
			)
			// create space uses CheckPermission endpoint
			cs3permissionsclient.On("CheckPermission", mock.Anything, mock.Anything, mock.Anything).Return(&cs3permissions.CheckPermissionResponse{
				Status: &v1beta11.Status{Code: v1beta11.Code_CODE_OK},
			}, nil).Times(1)

			// setup fs
			pub, con = make(chan interface{}), make(chan interface{})
			if usePosix {
				probe := filepath.Join(tmpRoot, ".xattr-probe")
				Expect(os.WriteFile(probe, nil, 0600)).To(Succeed())
				if err := xattr.Set(probe, "user.reva-test", []byte("1")); err != nil {
					Skip(fmt.Sprintf("extended attributes not supported on %s: %v", tmpRoot, err))
				}
				Expect(os.Remove(probe)).To(Succeed())

				idBucket := "async-upload-ids-" + uuid.NewString()
				historyBucket := "async-upload-history-" + uuid.NewString()
				idKV, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: idBucket})
				Expect(err).ToNot(HaveOccurred())
				historyKV, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: historyBucket})
				Expect(err).ToNot(HaveOccurred())
				idCache, err := idcache.NewStoreIDCache(idKV)
				Expect(err).ToNot(HaveOccurred())
				historyCache, err := idcache.NewStoreIDCache(historyKV)
				Expect(err).ToNot(HaveOccurred())

				posixConfig := config
				posixConfig["metadata_backend"] = "hybrid"
				posixConfig["permissionssvc"] = "any"
				posixConfig["personalspacepath_template"] = "users/{{.User.Username}}"
				posixConfig["generalspacepath_template"] = "projects/{{.SpaceId}}"
				posixConfig["idcache"] = map[string]interface{}{
					"cache_store": "nats-js-kv", "cache_database": idBucket,
				}
				po, err := posixoptions.New(posixConfig)
				Expect(err).ToNot(HaveOccurred())
				fs, err = posix.New(po, stream.Chan{pub, con}, idCache, historyCache, &zerolog.Logger{})
				Expect(err).ToNot(HaveOccurred())
			} else {
				lu = lookup.New(metadata.NewMessagePackBackend(o.FileMetadataCache), o, &timemanager.Manager{})
				pmock = &mocks.PermissionsChecker{}
				bs = &nodemocks.Blobstore{}
				blobs = map[string][]byte{}
				p := permissions.NewPermissions(pmock, permissionsSelector)
				pmock.On("AssemblePermissions", mock.Anything, mock.Anything).
					Return(&provider.ResourcePermissions{
						Stat:                 true,
						GetQuota:             true,
						InitiateFileDownload: true,
						InitiateFileUpload:   true,
						ListContainer:        true,
						ListFileVersions:     true,
					}, nil)
				tp := tree.New(lu, bs, o, p, store.Create(), &zerolog.Logger{})
				aspects := aspects.Aspects{
					Lookup:      lu,
					Tree:        tp,
					Permissions: p,
					EventStream: stream.Chan{pub, con},
					Trashbin:    &decomposedfs.DecomposedfsTrashbin{},
				}
				fs, err = decomposedfs.New(o, aspects, &zerolog.Logger{})
				Expect(err).ToNot(HaveOccurred())
			}

			resp, err := fs.CreateStorageSpace(ctx, &provider.CreateStorageSpaceRequest{Owner: user, Type: "personal"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.Status.Code).To(Equal(v1beta11.Code_CODE_OK))
			resID, err := storagespace.ParseID(resp.StorageSpace.Id.OpaqueId)
			Expect(err).ToNot(HaveOccurred())
			ref.ResourceId = &resID
			rootRef.ResourceId = &resID

			if !usePosix {
				bs.On("Upload", mock.AnythingOfType("*node.Node"), mock.AnythingOfType("string"), mock.Anything).
					Return(nil).
					Run(func(args mock.Arguments) {
						defer GinkgoRecover()
						n := args.Get(0).(*node.Node)
						data, err := os.ReadFile(args.Get(1).(string))
						Expect(err).ToNot(HaveOccurred())
						Expect(len(data)).To(Equal(int(n.Blobsize)))
						blobs[n.BlobID] = data
					})
				bs.On("Download", mock.AnythingOfType("*node.Node")).
					Return(func(n *node.Node) io.ReadCloser {
						return io.NopCloser(bytes.NewReader(blobs[n.BlobID]))
					}, nil)
			}

			// start upload of a file
			uploadIds, err := fs.InitiateUpload(ctx, ref, 10, map[string]string{})
			Expect(err).ToNot(HaveOccurred())
			Expect(len(uploadIds)).To(Equal(2))
			Expect(uploadIds["simple"]).ToNot(BeEmpty())
			Expect(uploadIds["tus"]).ToNot(BeEmpty())

			uploadRef := &provider.Reference{Path: "/" + uploadIds["simple"]}

			_, err = fs.Upload(ctx, storage.UploadRequest{
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
			if usePosix {
				_, _, err := fs.Download(ctx, ref, func(*provider.ResourceInfo) bool { return true })
				Expect(err).To(MatchError(ContainSubstring("too early")))
			} else {
				bs.AssertNumberOfCalls(GinkgoT(), "Upload", 0)
			}
		})

		AfterEach(func() {
			if o.Root != "" {
				//		os.RemoveAll(o.Root)
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
				if usePosix {
					_, reader, err := fs.Download(ctx, ref, func(*provider.ResourceInfo) bool { return true })
					Expect(err).ToNot(HaveOccurred())
					data, err := io.ReadAll(reader)
					Expect(err).ToNot(HaveOccurred())
					Expect(reader.Close()).To(Succeed())
					Expect(data).To(Equal(firstContent))
				} else {
					bs.AssertNumberOfCalls(GinkgoT(), "Upload", 1)
				}

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
				if !usePosix {
					bs.AssertNumberOfCalls(GinkgoT(), "Upload", 0)
				}

				// node gone
				resources, err = fs.ListFolder(ctx, rootRef, []string{}, []string{})
				Expect(err).ToNot(HaveOccurred())
				Expect(len(resources)).To(Equal(0))

				// bytes gone
				_, err = os.Stat(filepath.Join(o.Root, "uploads", uploadID))
				Expect(err).ToNot(BeNil())
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
				if !usePosix {
					bs.AssertNumberOfCalls(GinkgoT(), "Upload", 0)
				}

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
				uploadIds, err := fs.InitiateUpload(ctx, ref, 10, map[string]string{})
				Expect(err).ToNot(HaveOccurred())
				Expect(len(uploadIds)).To(Equal(2))
				Expect(uploadIds["simple"]).ToNot(BeEmpty())
				Expect(uploadIds["tus"]).ToNot(BeEmpty())

				uploadRef := &provider.Reference{Path: "/" + uploadIds["simple"]}

				_, err = fs.Upload(ctx, storage.UploadRequest{
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
				if !usePosix {
					bs.AssertNumberOfCalls(GinkgoT(), "Upload", 1)
				}

			})

			It("succeeds eventually, creating a new version", func() {
				succeedPostprocessing(uploadID)

				// version still existing
				revs, err := fs.ListRevisions(ctx, ref)
				Expect(err).To(BeNil())
				Expect(len(revs)).To(Equal(1))

				// blobstore now called twice - for original file and new version
				if !usePosix {
					bs.AssertNumberOfCalls(GinkgoT(), "Upload", 2)
				}

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
				if !usePosix {
					bs.AssertNumberOfCalls(GinkgoT(), "Upload", 1)
				}
			})

		})
		When("Two uploads are processed in parallel", func() {
			var secondUploadID string

			JustBeforeEach(func() {
				// upload again
				uploadIds, err := fs.InitiateUpload(ctx, ref, 20, map[string]string{})
				Expect(err).ToNot(HaveOccurred())
				Expect(len(uploadIds)).To(Equal(2))
				Expect(uploadIds["simple"]).ToNot(BeEmpty())
				Expect(uploadIds["tus"]).ToNot(BeEmpty())

				uploadRef := &provider.Reference{Path: "/" + uploadIds["simple"]}

				_, err = fs.Upload(ctx, storage.UploadRequest{
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

				fileAttributesMatchContents(ref)
				failPostprocessing(uploadID, events.PPOutcomeDelete)

				// check processing status
				_, _, size = fileStatus()
				// size should still match the second upload
				Expect(size).To(Equal(len(secondContent)))

				// parent size should still match second upload as well
				Expect(parentSize()).To(Equal(len(secondContent)))
				fileAndRevisionAttributesMatchContents()
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

				fileAndRevisionAttributesMatchContents()
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

				fileAndRevisionAttributesMatchContents()
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
				fileAndRevisionAttributesMatchContents()
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

				if usePosix {
					// FIXME: The failed latest upload does not restore the previous processing ID,
					// so the real posix tree finalizes the older upload as a revision.
					Expect(revisionCount()).To(Equal(1))
				} else {
					Expect(revisionCount()).To(Equal(0))
				}
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

				// check file has been removed
				// if all uploads have been processed with outcome delete -> delete the file
				// exists, _, _ := fileStatus()
				// FIXME this should be false, but we are not deleting the resource
				// Expect(exists).To(BeFalse())

				// parent size should be 0
				// FIXME we are not correctly reverting the sizediff
				// Expect(parentSize()).To(Equal(0))
			})

			It("the first can fail after the second fails", func() {
				failPostprocessing(secondUploadID, events.PPOutcomeDelete)

				_, status, size := fileStatus()
				// check processing status has been removed because the most recent upload finished and can be downloaded
				Expect(status).To(Equal(""))
				// size should match the first upload
				Expect(size).To(Equal(len(firstContent)))

				// parent size should match second first as well
				Expect(parentSize()).To(Equal(len(firstContent)))

				failPostprocessing(uploadID, events.PPOutcomeDelete)

				// check file has been removed
				// if all uploads have been processed with outcome delete -> delete the file
				// exists, _, _ := fileStatus()
				// FIXME this should be false, but we are not deleting the resource
				// Expect(exists).To(BeFalse())

				// parent size should be 0
				// FIXME we are not correctly reverting the sizediff
				// Expect(parentSize()).To(Equal(0))
			})
		})
	})
	Describe(name, decorators...)
}

func init() {
	//	defineAsyncUploadTests("Async file uploads (decomposedfs/messagepack)", false)
	defineAsyncUploadTests("Async file uploads (posixfs/hybrid)", true)
}
