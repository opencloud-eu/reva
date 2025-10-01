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

package decomposedfs

import (
	"context"
	"os"
	"strings"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	cs3permissions "github.com/cs3org/go-cs3apis/cs3/permissions/v1beta1"
	v1beta11 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	ruser "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/events"
	"github.com/opencloud-eu/reva/v2/pkg/events/stream"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storage/cache"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/aspects"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/lookup"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/metadata"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/metadata/prefixes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/node"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/options"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/permissions"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/permissions/mocks"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/timemanager"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/tree"
	treemocks "github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/tree/mocks"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	"github.com/opencloud-eu/reva/v2/pkg/store"
	"github.com/opencloud-eu/reva/v2/tests/helpers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These specs pin the DeleteRevision event handling: a nil timestamp reverts
// a stuck node to the state of its latest revision (or purges the node when
// there is none), and nodes that are not stuck are ignored, so that a
// redelivered event is a no-op.
var _ = Describe("DeleteRevision event", func() {
	var (
		ref       *provider.Reference
		fileResID *provider.ResourceId
		user      *userpb.User
		ctx       context.Context

		pub, con chan interface{}
		fs       storage.FS
		o        *options.Options
		lu       *lookup.Lookup

		nodeState = func() (exists bool, blobID string, size int64, processing bool) {
			n, err := lu.NodeFromID(ctx, fileResID)
			Expect(err).ToNot(HaveOccurred())
			if !n.Exists {
				return false, "", 0, false
			}
			processing = n.IsProcessing(ctx)
			return true, n.BlobID, n.Blobsize, processing
		}

		waitFor = func(f func() bool) {
			Eventually(f, "5s", "10ms").Should(BeTrue())
		}

		// createRevisionAt captures the node's current blob metadata in a new
		// revision, like the upload store does
		createRevisionAt = func(n *node.Node, version string) error {
			revPath := lu.InternalPath(n.SpaceID, n.ID+node.RevisionIDDelimiter+version)
			f, err := os.OpenFile(revPath, os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			_ = f.Close()

			return lu.CopyMetadata(ctx, n.InternalPath(), revPath, func(attributeName string, value []byte) (newValue []byte, copy bool) {
				return value, strings.HasPrefix(attributeName, prefixes.ChecksumPrefix) ||
					attributeName == prefixes.TypeAttr ||
					attributeName == prefixes.BlobIDAttr ||
					attributeName == prefixes.BlobsizeAttr ||
					attributeName == prefixes.MTimeAttr
			}, true)
		}
	)

	BeforeEach(func() {
		tmpRoot, err := helpers.TempDir("reva-unit-tests-*-root")
		Expect(err).ToNot(HaveOccurred())

		o, err = options.New(map[string]interface{}{
			"root":             tmpRoot,
			"asyncfileuploads": true,
		})
		Expect(err).ToNot(HaveOccurred())

		user = &userpb.User{
			Id: &userpb.UserId{
				Idp:      "idp",
				OpaqueId: "u-s-e-r-id",
				Type:     userpb.UserType_USER_TYPE_PRIMARY,
			},
			Username: "username",
		}
		ctx = ruser.ContextSetUser(context.Background(), user)
		ref = &provider.Reference{
			ResourceId: &provider.ResourceId{
				SpaceId: "u-s-e-r-id",
			},
			Path: "/file",
		}

		lu = lookup.New(metadata.NewXattrsBackend(o.Root, cache.Config{}), o, &timemanager.Manager{})
		pmock := &mocks.PermissionsChecker{}

		cs3permissionsclient := &mocks.CS3PermissionsClient{}
		pool.RemoveSelector("PermissionsSelector" + "any")
		permissionsSelector := pool.GetSelector[cs3permissions.PermissionsAPIClient](
			"PermissionsSelector",
			"any",
			func(cc grpc.ClientConnInterface) cs3permissions.PermissionsAPIClient {
				return cs3permissionsclient
			},
		)
		bs := &treemocks.Blobstore{}

		// create space uses CheckPermission endpoint
		cs3permissionsclient.On("CheckPermission", mock.Anything, mock.Anything, mock.Anything).Return(&cs3permissions.CheckPermissionResponse{
			Status: &v1beta11.Status{Code: v1beta11.Code_CODE_OK},
		}, nil).Times(1)

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
		pub, con = make(chan interface{}, 16), make(chan interface{})
		tp := tree.New(lu, bs, o, store.Create(), &zerolog.Logger{})

		fs, err = New(o, aspects.Aspects{
			Lookup:      lu,
			Tree:        tp,
			Permissions: permissions.NewPermissions(pmock, permissionsSelector),
			EventStream: stream.Chan{pub, con},
			Trashbin:    &DecomposedfsTrashbin{},
		}, &zerolog.Logger{})
		Expect(err).ToNot(HaveOccurred())

		resp, err := fs.CreateStorageSpace(ctx, &provider.CreateStorageSpaceRequest{Owner: user, Type: "personal"})
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Status.Code).To(Equal(v1beta11.Code_CODE_OK))
		resID, err := storagespace.ParseID(resp.StorageSpace.Id.OpaqueId)
		Expect(err).ToNot(HaveOccurred())
		ref.ResourceId = &resID

		// create a file and resolve its resource id
		Expect(fs.TouchFile(ctx, ref, false, "")).To(Succeed())
		n, err := lu.NodeFromResource(ctx, ref)
		Expect(err).ToNot(HaveOccurred())
		Expect(n.Exists).To(BeTrue())
		fileResID = &provider.ResourceId{
			SpaceId:  ref.ResourceId.SpaceId,
			OpaqueId: n.ID,
		}
	})

	AfterEach(func() {
		if o.Root != "" {
			os.RemoveAll(o.Root)
		}
		close(con)
	})

	// createStuckRevision captures the file's current blob metadata in a new
	// revision, then simulates a failed upload taking over the node and
	// leaving it stuck in processing
	createStuckRevision := func() {
		n, err := lu.NodeFromID(ctx, fileResID)
		Expect(err).ToNot(HaveOccurred())

		n.BlobID = "orig-blobid"
		n.Blobsize = 1234
		Expect(n.SetXattrs(n.NodeMetadata(ctx), true)).To(Succeed())

		Expect(createRevisionAt(n, "2024-01-01T00:00:00Z")).To(Succeed())

		// simulate a failed upload overwriting the node's blob metadata
		n.BlobID = "bad-blobid"
		n.Blobsize = 42
		Expect(n.SetXattrs(n.NodeMetadata(ctx), true)).To(Succeed())

		Expect(n.SetXattr(ctx, prefixes.StatusPrefix, []byte(node.ProcessingStatus+"upload-1"))).To(Succeed())
		Expect(n.IsProcessing(ctx)).To(BeTrue())
	}

	It("reverts a stuck node to the state of its latest revision", func() {
		createStuckRevision()

		con <- events.DeleteRevision{ResourceID: fileResID}

		var exists bool
		var blobID string
		var size int64
		var processing bool
		waitFor(func() bool {
			exists, blobID, size, processing = nodeState()
			return !processing
		})
		Expect(exists).To(BeTrue())
		Expect(blobID).To(Equal("orig-blobid"))
		Expect(size).To(Equal(int64(1234)))

		// the reverted revision is consumed
		revs, err := fs.ListRevisions(ctx, ref)
		Expect(err).ToNot(HaveOccurred())
		Expect(revs).To(BeEmpty())
	})

	It("purges a stuck node when there is no revision", func() {
		n, err := lu.NodeFromID(ctx, fileResID)
		Expect(err).ToNot(HaveOccurred())
		Expect(n.SetXattr(ctx, prefixes.StatusPrefix, []byte(node.ProcessingStatus+"upload-1"))).To(Succeed())

		con <- events.DeleteRevision{ResourceID: fileResID}

		var exists bool
		waitFor(func() bool {
			exists, _, _, _ = nodeState()
			return !exists
		})
	})

	It("ignores a node that is not stuck", func() {
		createStuckRevision()

		n, err := lu.NodeFromID(ctx, fileResID)
		Expect(err).ToNot(HaveOccurred())
		Expect(n.UnmarkProcessing(ctx, "upload-1")).To(Succeed())

		con <- events.DeleteRevision{ResourceID: fileResID}

		Consistently(func(g Gomega) {
			exists, blobID, size, processing := nodeState()
			g.Expect(exists).To(BeTrue())
			g.Expect(blobID).To(Equal("bad-blobid"))
			g.Expect(size).To(Equal(int64(42)))
			g.Expect(processing).To(BeFalse())
		}, "1s", "50ms").Should(Succeed())

		// the revision survives
		revs, err := fs.ListRevisions(ctx, ref)
		Expect(err).ToNot(HaveOccurred())
		Expect(len(revs)).To(Equal(1))
	})

	It("is a no-op when the event is redelivered", func() {
		createStuckRevision()

		con <- events.DeleteRevision{ResourceID: fileResID}

		var exists bool
		var blobID string
		waitFor(func() bool {
			exists, blobID, _, _ = nodeState()
			return exists && blobID == "orig-blobid"
		})

		// redelivery of the same event must not change anything
		con <- events.DeleteRevision{ResourceID: fileResID}

		Consistently(func(g Gomega) {
			exists, blobID, size, processing := nodeState()
			g.Expect(exists).To(BeTrue())
			g.Expect(blobID).To(Equal("orig-blobid"))
			g.Expect(size).To(Equal(int64(1234)))
			g.Expect(processing).To(BeFalse())
		}, "1s", "50ms").Should(Succeed())
	})
})
