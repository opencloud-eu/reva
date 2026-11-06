// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package decomposedfs_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	cs3permissions "github.com/cs3org/go-cs3apis/cs3/permissions/v1beta1"
	v1beta11 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	ruser "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/decomposed/blobstore"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/aspects"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/lookup"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/options"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/permissions"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/permissions/mocks"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/timemanager"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/tree"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	"github.com/opencloud-eu/reva/v2/pkg/store"
	"github.com/opencloud-eu/reva/v2/tests/helpers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
	tusd "github.com/tus/tusd/v2/pkg/handler"
	"google.golang.org/grpc"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// watchedBlobstore lets a test look at the node while its blob is stored
type watchedBlobstore struct {
	*blobstore.Blobstore
	onUpload func(blob *node.Node)
}

func (bs *watchedBlobstore) Upload(n *node.Node, source, copyTarget string) error {
	if bs.onUpload != nil {
		bs.onUpload(n)
	}
	return bs.Blobstore.Upload(n, source, copyTarget)
}

// faultyBackend fails writes that switch a node to another blob, either
// before or after writing. It can also run a hook before the next lock.
type faultyBackend struct {
	metadata.Backend
	failSwitch string
	beforeLock func()
}

func (b *faultyBackend) Lock(n metadata.MetadataNode) (metadata.UnlockFunc, error) {
	if hook := b.beforeLock; hook != nil {
		b.beforeLock = nil
		hook()
	}
	return b.Backend.Lock(n)
}

func (b *faultyBackend) SetMultiple(ctx context.Context, n metadata.MetadataNode, attribs map[string][]byte) error {
	if _, ok := attribs[prefixes.BlobIDAttr]; !ok || b.failSwitch == "" {
		return b.Backend.SetMultiple(ctx, n, attribs)
	}
	if b.failSwitch == "after" {
		if err := b.Backend.SetMultiple(ctx, n, attribs); err != nil {
			return err
		}
	}
	return errors.New("metadata write failed")
}

var _ = Describe("Synchronous uploads", func() {
	var (
		ctx context.Context
		ref *provider.Reference
		fs  storage.FS
		o   *options.Options
		lu  *lookup.Lookup
		bs  *watchedBlobstore
		mb  *faultyBackend
	)

	BeforeEach(func() {
		user := &userpb.User{
			Id: &userpb.UserId{
				Idp:      "idp",
				OpaqueId: "u-s-e-r-id",
				Type:     userpb.UserType_USER_TYPE_PRIMARY,
			},
			Username: "username",
		}
		ctx = ruser.ContextSetUser(context.Background(), user)

		root, err := helpers.TempDir("reva-unit-tests-*-root")
		Expect(err).ToNot(HaveOccurred())
		// configured like the system storage, plus tree times to see propagation
		o, err = options.New(map[string]any{
			"root":                root,
			"disable_versioning":  true,
			"treetime_accounting": true,
			"filemetadatacache": map[string]any{
				"cache_database": root,
			},
		})
		Expect(err).ToNot(HaveOccurred())
		mb = &faultyBackend{Backend: metadata.NewMessagePackBackend(o.FileMetadataCache)}
		lu = lookup.New(mb, o, &timemanager.Manager{})

		blobs, err := blobstore.New(root)
		Expect(err).ToNot(HaveOccurred())
		bs = &watchedBlobstore{Blobstore: blobs}

		pmock := &mocks.PermissionsChecker{}
		pmock.On("AssemblePermissions", mock.Anything, mock.Anything).Return(&provider.ResourcePermissions{
			Stat:                 true,
			GetQuota:             true,
			AddGrant:             true,
			ListContainer:        true,
			InitiateFileUpload:   true,
			InitiateFileDownload: true,
		}, nil)
		cs3permissionsclient := &mocks.CS3PermissionsClient{}
		cs3permissionsclient.On("CheckPermission", mock.Anything, mock.Anything, mock.Anything).Return(&cs3permissions.CheckPermissionResponse{
			Status: &v1beta11.Status{Code: v1beta11.Code_CODE_OK},
		}, nil)
		pool.RemoveSelector("PermissionsSelector" + "any")
		permissionsSelector := pool.GetSelector[cs3permissions.PermissionsAPIClient](
			"PermissionsSelector",
			"any",
			func(cc grpc.ClientConnInterface) cs3permissions.PermissionsAPIClient {
				return cs3permissionsclient
			},
		)

		fs, err = decomposedfs.New(o, aspects.Aspects{
			Lookup:            lu,
			Tree:              tree.New(lu, bs, o, permissions.Permissions{}, store.Create(), &zerolog.Logger{}),
			Permissions:       permissions.NewPermissions(pmock, permissionsSelector),
			Trashbin:          &decomposedfs.DecomposedfsTrashbin{},
			DisableVersioning: true,
			SeparateBlobs:     true,
		}, &zerolog.Logger{})
		Expect(err).ToNot(HaveOccurred())

		resp, err := fs.CreateStorageSpace(ctx, &provider.CreateStorageSpaceRequest{Owner: user, Type: "personal"})
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Status.Code).To(Equal(v1beta11.Code_CODE_OK))
		spaceID, err := storagespace.ParseID(resp.StorageSpace.Id.OpaqueId)
		Expect(err).ToNot(HaveOccurred())
		ref = &provider.Reference{ResourceId: &spaceID, Path: "/shares.json"}
	})

	AfterEach(func() {
		if o != nil && o.Root != "" {
			_ = os.RemoveAll(o.Root)
		}
	})

	upload := func(content string, metadata map[string]string) (string, error) {
		ids, err := fs.InitiateUpload(ctx, ref, int64(len(content)), metadata)
		if err != nil {
			return "", err
		}
		_, err = fs.Upload(ctx, storage.UploadRequest{
			Ref:    &provider.Reference{Path: "/" + ids["simple"]},
			Body:   io.NopCloser(strings.NewReader(content)),
			Length: int64(len(content)),
		}, nil)
		return ids["simple"], err
	}

	download := func() (string, error) {
		_, r, err := fs.Download(ctx, ref, func(*provider.ResourceInfo) bool { return true })
		if err != nil {
			return "", err
		}
		defer r.Close()
		b, err := io.ReadAll(r)
		return string(b), err
	}

	file := func() *node.Node {
		n, err := lu.NodeFromResource(ctx, ref)
		Expect(err).ToNot(HaveOccurred())
		return n
	}

	uploadFiles := func() []string {
		entries, err := os.ReadDir(filepath.Join(o.Root, "uploads"))
		Expect(err).ToNot(HaveOccurred())
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names
	}

	It("stores the blob before the node points to it", func() {
		_, err := upload("v1", nil)
		Expect(err).ToNot(HaveOccurred())

		stored := false
		bs.onUpload = func(blob *node.Node) {
			current := file()
			Expect(current.IsProcessing(ctx)).To(BeFalse())
			Expect(current.BlobID).ToNot(Equal(blob.BlobID))
			stored = true
		}
		id, err := upload("v2", nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(stored).To(BeTrue())

		Expect(file().IsProcessing(ctx)).To(BeFalse())
		Expect(download()).To(Equal("v2"))
		Expect(uploadFiles()).ToNot(ContainElement(HavePrefix(id)))
	})

	It("keeps the old content and drops the new blob when the upload is rejected", func() {
		_, err := upload("v1", nil)
		Expect(err).ToNot(HaveOccurred())

		id, err := upload("v2", map[string]string{"if-match": `"not-the-etag"`})
		Expect(err).To(HaveOccurred())

		Expect(file().IsProcessing(ctx)).To(BeFalse())
		Expect(download()).To(Equal("v1"))
		Expect(bs.Path(&node.Node{BaseNode: node.BaseNode{SpaceID: ref.ResourceId.SpaceId}, BlobID: id})).ToNot(BeAnExistingFile())
	})

	It("keeps the new blob when the node was written despite an error", func() {
		_, err := upload("v1", nil)
		Expect(err).ToNot(HaveOccurred())

		mb.failSwitch = "after"
		_, err = upload("v2", nil)
		Expect(err).To(HaveOccurred())
		mb.failSwitch = ""

		Expect(download()).To(Equal("v2"))
	})

	Context("with a node left processing by an interrupted upload", func() {
		var v1BlobID string

		BeforeEach(func() {
			_, err := upload("v1", nil)
			Expect(err).ToNot(HaveOccurred())
			v1BlobID = file().BlobID
		})

		mark := func(blobID, status string) {
			attrs := node.Attributes{}
			attrs.SetString(prefixes.BlobIDAttr, blobID)
			attrs.SetString(prefixes.StatusPrefix, status)
			Expect(file().SetXattrsWithContext(ctx, attrs)).To(Succeed())
		}

		// interrupt receives content and switches the node over to it the way
		// older versions did, then stops before the blob is stored
		interrupt := func(content string) string {
			ids, err := fs.InitiateUpload(ctx, ref, int64(len(content)), nil)
			Expect(err).ToNot(HaveOccurred())
			session, err := fs.(tusd.DataStore).GetUpload(ctx, ids["tus"])
			Expect(err).ToNot(HaveOccurred())
			_, err = session.WriteChunk(ctx, 0, strings.NewReader(content))
			Expect(err).ToNot(HaveOccurred())

			attrs := node.Attributes{}
			attrs.SetInt64(prefixes.BlobsizeAttr, int64(len(content)))
			Expect(file().SetXattrsWithContext(ctx, attrs)).To(Succeed())
			mark(ids["tus"], node.ProcessingStatus+ids["tus"])
			return ids["tus"]
		}

		age := func(id string) {
			then := time.Now().Add(-time.Hour)
			Expect(os.Chtimes(filepath.Join(o.Root, "uploads", id+".info"), then, then)).To(Succeed())
		}

		It("finishes the upload when the file is read", func() {
			id := interrupt("v2")
			age(id)

			Expect(download()).To(Equal("v2"))
			Expect(file().IsProcessing(ctx)).To(BeFalse())
			Expect(uploadFiles()).ToNot(ContainElement(HavePrefix(id)))
		})

		It("propagates the finished upload", func() {
			id := interrupt("v2")
			age(id)
			treeTime := func() time.Time {
				root, err := lu.NodeFromResource(ctx, &provider.Reference{ResourceId: ref.ResourceId})
				Expect(err).ToNot(HaveOccurred())
				t, err := root.GetTMTime(ctx)
				Expect(err).ToNot(HaveOccurred())
				return t
			}
			before := treeTime()

			Expect(download()).To(Equal("v2"))
			Expect(treeTime()).To(BeTemporally(">", before))
		})

		It("drops the marker when the blob was stored already", func() {
			mark(v1BlobID, node.ProcessingStatus+v1BlobID)

			Expect(download()).To(Equal("v1"))
			Expect(file().IsProcessing(ctx)).To(BeFalse())
		})

		It("leaves an upload alone that might still be running", func() {
			interrupt("v2")

			_, err := download()
			Expect(err).To(MatchError(errtypes.TooEarly("file is still being processed")))
		})

		It("leaves an upload alone that started while waiting for the lock", func() {
			id := interrupt("v2")
			age(id)

			var next string
			mb.beforeLock = func() { next = interrupt("v3") }
			_, err := download()
			Expect(err).To(MatchError(errtypes.TooEarly("file is still being processed")))
			Expect(file().ProcessingID(ctx)).To(Equal(next))
		})

		It("keeps the marker when the content is lost", func() {
			mark("lost", node.ProcessingStatus+"lost")

			_, err := download()
			Expect(err).To(MatchError(errtypes.TooEarly("file is still being processed")))
			Expect(file().IsProcessing(ctx)).To(BeTrue())
		})

		It("leaves markers alone that do not belong to an upload", func() {
			mark(v1BlobID, node.ProcessingStatus)

			_, err := download()
			Expect(err).To(MatchError(errtypes.TooEarly("file is still being processed")))
		})

		It("is cleared by the next upload", func() {
			interrupt("v2")

			_, err := upload("v3", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(file().IsProcessing(ctx)).To(BeFalse())
			Expect(download()).To(Equal("v3"))
		})

		It("stays recoverable when the next upload fails to write the node", func() {
			id := interrupt("v2")
			age(id)

			mb.failSwitch = "before"
			_, err := upload("v3", nil)
			Expect(err).To(HaveOccurred())
			mb.failSwitch = ""

			Expect(file().IsProcessing(ctx)).To(BeTrue())
			Expect(download()).To(Equal("v2"))
		})
	})
})
