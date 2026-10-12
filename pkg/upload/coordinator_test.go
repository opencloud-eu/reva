package upload

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	ctxpkg "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
)

const (
	spaceID   = "space-1"
	nodeID    = "node-1"
	parentID  = "parent-1"
	body      = "hello coordinator"
	bodyLen   = int64(len(body))
	mountID   = "storage-1"
	spaceRoot = "space-1"
)

// stagedSession primes a session the way InitiateUpload leaves one, bytes staged.
func stagedSession(ctx context.Context, store *FileStore, nodeExists bool) Session {
	session := store.New(ctx)
	session.SetMetadata("providerID", mountID)
	session.SetMetadata("filename", "report.docx")
	session.SetMetadata("mtime", utils.TimeToOCMtime(utils.TSToTime(utils.TSNow())))
	session.SetStorageValue("NodeName", "report.docx")
	session.SetStorageValue("Dir", "/Shares/project")
	session.SetStorageValue("SpaceRoot", spaceRoot)
	session.SetStorageValue("NodeParentId", parentID)
	session.SetStorageValue("NodeId", nodeID)
	if nodeExists {
		session.SetStorageValue("NodeExists", "true")
	}
	session.SetExecutant(&userpb.User{
		Id:       &userpb.UserId{OpaqueId: "alice", Idp: "idp.example.com"},
		Username: "alice",
	})
	session.SetSize(bodyLen)

	Expect(session.TouchBin()).To(Succeed())
	written, err := session.WriteChunk(ctx, 0, strings.NewReader(body))
	Expect(err).ToNot(HaveOccurred())
	Expect(written).To(Equal(bodyLen))
	Expect(session.Persist(ctx)).To(Succeed())
	return session
}

var _ = Describe("coordinator", func() {
	var (
		ctx   context.Context
		store *FileStore
		fs    *fakeFS
		c     *coordinator
	)

	newSession := func(nodeExists bool) Session {
		return stagedSession(ctx, store, nodeExists)
	}

	BeforeEach(func() {
		ctx = ctxpkg.ContextSetUser(context.Background(), &userpb.User{
			Id: &userpb.UserId{OpaqueId: "alice", Idp: "idp.example.com"},
		})
		store = NewFileStore(filepath.Join(GinkgoT().TempDir(), "uploads"), TokenOptions{}, nopLog())
		Expect(store.Setup()).To(Succeed())

		fs = &fakeFS{
			touched: &provider.ResourceId{StorageId: mountID, SpaceId: spaceID, OpaqueId: nodeID},
			md: &provider.ResourceInfo{
				Id:    &provider.ResourceId{StorageId: mountID, SpaceId: spaceID, OpaqueId: nodeID},
				Etag:  "etag-after-commit",
				Size:  uint64(bodyLen),
				Mtime: utils.TSNow(),
			},
		}
		c = NewCoordinator(fs, store, "", nil)
	})

	Describe("finishUpload", func() {
		Context("for a new file", func() {
			// PrepareUpload marks the node in its own write, so there is no separate mark.
			It("touches, prepares and commits in that order", func() {
				session := newSession(false)

				ri, err := c.finishUpload(ctx, session)

				Expect(err).ToNot(HaveOccurred())
				Expect(ri.GetEtag()).To(Equal("etag-after-commit"))
				Expect(fs.calls).To(Equal([]string{
					"TouchFile(markprocessing=false)",
					"PrepareUpload(size=17)",
					"CommitUpload(length=17)",
					"MarkProcessing(false)",
					"GetMD()",
				}))
			})

			It("removes the staged files once committed", func() {
				session := newSession(false)

				_, err := c.finishUpload(ctx, session)
				Expect(err).ToNot(HaveOccurred())

				_, err = os.Stat(session.BinPath())
				Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
			})

			It("replaces the placeholder node id with the one TouchFile returned", func() {
				session := newSession(false)
				fs.touched = &provider.ResourceId{StorageId: mountID, SpaceId: spaceID, OpaqueId: "real-node-id"}

				_, err := c.finishUpload(ctx, session)

				Expect(err).ToNot(HaveOccurred())
				Expect(session.NodeID()).To(Equal("real-node-id"))
			})

			// PrepareUpload propagates the node, so a second walk would be wasted.
			It("tells TouchFile not to propagate", func() {
				session := newSession(false)

				_, err := c.finishUpload(ctx, session)

				Expect(err).ToNot(HaveOccurred())
				Expect(fs.touchSkippedPropagation).To(BeTrue())
			})

			// TouchFile has created the node, so PrepareUpload must not create another.
			It("does not tell PrepareUpload where the file goes", func() {
				session := newSession(false)

				_, err := c.finishUpload(ctx, session)

				Expect(err).ToNot(HaveOccurred())
				Expect(fs.prepareInfo.ParentID).To(BeEmpty())
				Expect(fs.prepareInfo.Name).To(BeEmpty())
			})
		})

		Context("for a new file on a driver whose PrepareUpload creates it", func() {
			BeforeEach(func() {
				c = NewCoordinator(&fakeCreatorFS{fakeFS: fs}, store, "", nil)
			})

			It("prepares and commits without a TouchFile", func() {
				session := newSession(false)

				_, err := c.finishUpload(ctx, session)

				Expect(err).ToNot(HaveOccurred())
				Expect(fs.calls).To(Equal([]string{
					"PrepareUpload(size=17)",
					"CommitUpload(length=17)",
					"MarkProcessing(false)",
					"GetMD()",
				}))
			})

			It("tells PrepareUpload where the file goes", func() {
				session := newSession(false)

				_, err := c.finishUpload(ctx, session)

				Expect(err).ToNot(HaveOccurred())
				Expect(fs.prepareInfo.ParentID).To(Equal(parentID))
				Expect(fs.prepareInfo.Name).To(Equal("report.docx"))
			})

			// The driver creates the node under the placeholder, which so becomes its id,
			// and there is no TouchFile to report the space owner. The commit may run in
			// another process, which only has the saved session.
			It("saves the node id minted at initiate and the space owner PrepareUpload reported", func() {
				session := newSession(false)
				c.async = true
				c.pub = &fakePublisher{}
				fs.prepared = &storage.PrepareUploadResult{
					SizeDiff:   bodyLen,
					SpaceOwner: &userpb.UserId{OpaqueId: "manager-1", Idp: "idp.example.com", Type: userpb.UserType_USER_TYPE_PRIMARY},
				}

				_, err := c.finishUpload(ctx, session)
				Expect(err).ToNot(HaveOccurred())

				saved, err := store.Get(ctx, session.ID())
				Expect(err).ToNot(HaveOccurred())
				Expect(saved.NodeID()).To(Equal(nodeID))
				Expect(saved.SpaceOwner().GetOpaqueId()).To(Equal("manager-1"))
			})

			// The parent went away, or the share was revoked, while bytes were in flight.
			It("reports a missing parent as a failed precondition", func() {
				session := newSession(false)
				fs.prepareErr = errtypes.NotFound("parent-1")

				_, err := c.finishUpload(ctx, session)

				Expect(err).To(BeAssignableToTypeOf(errtypes.PreconditionFailed("")))
				Expect(fs.calls).To(Equal([]string{"PrepareUpload(size=17)"}))
				_, err = os.Stat(session.BinPath())
				Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
			})

			It("passes any other failure through", func() {
				session := newSession(false)
				fs.prepareErr = errtypes.InsufficientStorage("quota exceeded")

				_, err := c.finishUpload(ctx, session)

				Expect(err).To(BeAssignableToTypeOf(errtypes.InsufficientStorage("")))
			})

			It("does not tell PrepareUpload where an overwrite goes", func() {
				session := newSession(true)

				_, err := c.finishUpload(ctx, session)

				Expect(err).ToNot(HaveOccurred())
				Expect(fs.prepareInfo.ParentID).To(BeEmpty())
				Expect(fs.prepareInfo.Name).To(BeEmpty())
			})

			// The node itself went away: that is not the parent's precondition.
			It("leaves an overwrite's missing node not found", func() {
				session := newSession(true)
				fs.prepareErr = errtypes.NotFound("node-1")

				_, err := c.finishUpload(ctx, session)

				Expect(err).To(BeAssignableToTypeOf(errtypes.NotFound("")))
			})
		})

		Context("for an overwrite", func() {
			It("skips TouchFile", func() {
				session := newSession(true)

				_, err := c.finishUpload(ctx, session)

				Expect(err).ToNot(HaveOccurred())
				Expect(fs.calls).ToNot(ContainElement(ContainSubstring("TouchFile")))
				Expect(fs.calls[0]).To(Equal("PrepareUpload(size=17)"))
			})
		})

		// PrepareUpload undoes its own writes, purges a new file's node and marks the
		// node only on success, so the coordinator has nothing to roll back or unmark.
		Context("when PrepareUpload fails", func() {
			It("leaves the cleanup of a new file's node to PrepareUpload", func() {
				session := newSession(false)
				fs.prepareErr = errors.New("insufficient storage")

				_, err := c.finishUpload(ctx, session)

				Expect(err).To(MatchError("insufficient storage"))
				Expect(fs.calls).To(Equal([]string{
					"TouchFile(markprocessing=false)",
					"PrepareUpload(size=17)",
				}))
			})

			It("leaves an existing file to PrepareUpload as well", func() {
				session := newSession(true)
				fs.prepareErr = errors.New("precondition failed")

				_, err := c.finishUpload(ctx, session)

				Expect(err).To(MatchError("precondition failed"))
				Expect(fs.calls).To(Equal([]string{
					"PrepareUpload(size=17)",
				}))
			})

			It("removes the staged files", func() {
				session := newSession(false)
				fs.prepareErr = errors.New("insufficient storage")

				_, err := c.finishUpload(ctx, session)
				Expect(err).To(HaveOccurred())

				_, err = os.Stat(session.BinPath())
				Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
				_, err = store.Get(ctx, session.ID())
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when CommitUpload fails", func() {
			// PrepareUpload propagated the size optimistically.
			It("rolls back the size PrepareUpload reported", func() {
				session := newSession(true)
				fs.prepared = &storage.PrepareUploadResult{SizeDiff: bodyLen, VersionCreated: true}
				fs.commitErr = errors.New("blobstore unavailable")

				_, err := c.finishUpload(ctx, session)

				Expect(err).To(MatchError("blobstore unavailable"))
				Expect(fs.calls).To(Equal([]string{
					"PrepareUpload(size=17)",
					"CommitUpload(length=17)",
					"RollbackUpload(nodeExisted=true,sizeDiff=17)",
					"MarkProcessing(false)",
				}))
			})
		})

		Context("when the announced checksum does not match", func() {
			It("rejects the upload before the driver is called", func() {
				session := newSession(false)
				session.SetMetadata("checksum", "sha1 "+strings.Repeat("0", 40))
				Expect(session.Persist(ctx)).To(Succeed())

				_, err := c.finishUpload(ctx, session)

				Expect(err).To(HaveOccurred())
				Expect(fs.calls).To(BeEmpty())
			})

			It("removes the staged files", func() {
				session := newSession(false)
				session.SetMetadata("checksum", "sha1 "+strings.Repeat("0", 40))
				Expect(session.Persist(ctx)).To(Succeed())

				_, err := c.finishUpload(ctx, session)
				Expect(err).To(HaveOccurred())

				_, err = os.Stat(session.BinPath())
				Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
				_, err = store.Get(ctx, session.ID())
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when the rollback itself fails", func() {
			// The session is the only remaining record of what to undo, so throwing
			// it away would leave the quota consumed with nothing to reclaim it from.
			It("keeps the session for a retry", func() {
				session := newSession(true)
				fs.prepared = &storage.PrepareUploadResult{SizeDiff: bodyLen}
				fs.commitErr = errors.New("blobstore unavailable")
				fs.rollbackErr = errors.New("no such node")

				_, err := c.finishUpload(ctx, session)

				Expect(err).To(MatchError("blobstore unavailable"))
				_, getErr := store.Get(ctx, session.ID())
				Expect(getErr).ToNot(HaveOccurred())
				Expect(fs.calls).ToNot(ContainElement("MarkProcessing(false)"))
			})
		})

		// A node whose own metadata is unreadable can only be reached through the
		// ids the session recorded.
		Context("the rollback description", func() {
			It("carries the ids the driver needs to reach an orphaned node", func() {
				session := newSession(true)
				fs.prepared = &storage.PrepareUploadResult{SizeDiff: bodyLen}
				fs.commitErr = errors.New("blobstore unavailable")

				_, err := c.finishUpload(ctx, session)

				Expect(err).To(HaveOccurred())
				Expect(fs.rolledBack).To(Equal(storage.RollbackInfo{
					NodeExisted: true,
					SizeDiff:    bodyLen,
					NodeID:      nodeID,
					ParentID:    parentID,
					Filename:    "report.docx",
					Size:        bodyLen,
				}))
			})
		})
	})

	Describe("Upload", func() {
		It("stages the body and finishes the upload", func() {
			session := store.New(ctx)
			session.SetMetadata("providerID", mountID)
			session.SetMetadata("filename", "report.docx")
			session.SetStorageValue("SpaceRoot", spaceRoot)
			session.SetStorageValue("NodeId", nodeID)
			session.SetStorageValue("NodeParentId", parentID)
			session.SetExecutant(&userpb.User{Id: &userpb.UserId{OpaqueId: "alice"}})
			session.SetSize(bodyLen)
			Expect(session.TouchBin()).To(Succeed())
			Expect(session.Persist(ctx)).To(Succeed())

			ri, err := c.Upload(ctx, Request{
				Ref:    &provider.Reference{Path: "/" + session.ID()},
				Body:   io.NopCloser(strings.NewReader(body)),
				Length: bodyLen,
			}, nil)

			Expect(err).ToNot(HaveOccurred())
			Expect(ri.GetEtag()).To(Equal("etag-after-commit"))
			Expect(fs.calls).To(ContainElement("CommitUpload(length=17)"))
		})

		// An async finish keeps the session for postprocessing, so a PUT can reach it again.
		It("refuses a session whose bytes were already received", func() {
			c.async = true
			c.pub = &fakePublisher{}
			session := store.New(ctx)
			session.SetMetadata("providerID", mountID)
			session.SetMetadata("filename", "report.docx")
			session.SetStorageValue("SpaceRoot", spaceRoot)
			session.SetStorageValue("NodeId", nodeID)
			session.SetStorageValue("NodeParentId", parentID)
			session.SetExecutant(&userpb.User{Id: &userpb.UserId{OpaqueId: "alice"}})
			session.SetSize(bodyLen)
			Expect(session.TouchBin()).To(Succeed())
			Expect(session.Persist(ctx)).To(Succeed())
			put := func() error {
				_, err := c.Upload(ctx, Request{
					Ref:    &provider.Reference{Path: "/" + session.ID()},
					Body:   io.NopCloser(strings.NewReader(body)),
					Length: bodyLen,
				}, nil)
				return err
			}
			Expect(put()).To(Succeed())
			calls := len(fs.calls)

			Expect(put()).To(BeAssignableToTypeOf(errtypes.Aborted("")))

			Expect(fs.calls).To(HaveLen(calls), "the repeated PUT reached the driver")
			saved, err := store.Get(ctx, session.ID())
			Expect(err).ToNot(HaveOccurred(), "the session postprocessing needs was discarded")
			Expect(saved.Offset()).To(Equal(bodyLen))
			staged, err := os.Stat(session.BinPath())
			Expect(err).ToNot(HaveOccurred())
			Expect(staged.Size()).To(Equal(bodyLen), "the repeated PUT appended to the staged bytes")
		})

		It("rejects a body shorter than the declared length", func() {
			session := store.New(ctx)
			session.SetMetadata("providerID", mountID)
			session.SetStorageValue("SpaceRoot", spaceRoot)
			session.SetStorageValue("NodeId", nodeID)
			session.SetExecutant(&userpb.User{Id: &userpb.UserId{OpaqueId: "alice"}})
			Expect(session.TouchBin()).To(Succeed())
			Expect(session.Persist(ctx)).To(Succeed())

			_, err := c.Upload(ctx, Request{
				Ref:    &provider.Reference{Path: "/" + session.ID()},
				Body:   io.NopCloser(strings.NewReader("short")),
				Length: bodyLen,
			}, nil)

			Expect(err).To(HaveOccurred())
			// Nothing reached the driver, so the session can still be retried.
			Expect(fs.calls).To(BeEmpty())
		})

		It("refuses a chunked upload when no chunk folder is configured", func() {
			session := store.New(ctx)
			session.SetMetadata("providerID", mountID)
			session.SetStorageValue("SpaceRoot", spaceRoot)
			session.SetStorageValue("NodeId", nodeID)
			session.SetStorageValue("Chunk", "report.docx-chunking-abc-3-0")
			session.SetExecutant(&userpb.User{Id: &userpb.UserId{OpaqueId: "alice"}})
			Expect(session.TouchBin()).To(Succeed())
			Expect(session.Persist(ctx)).To(Succeed())

			_, err := c.Upload(ctx, Request{
				Ref:    &provider.Reference{Path: "/" + session.ID()},
				Body:   io.NopCloser(strings.NewReader(body)),
				Length: bodyLen,
			}, nil)

			Expect(err).To(HaveOccurred())
			Expect(fs.calls).To(BeEmpty())
		})
	})
})
