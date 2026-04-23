package idcache_test

import (
	"context"

	"github.com/opencloud-eu/reva/v2/pkg/storage/cache"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/idcache"
	helpers "github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/testhelpers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("IDCache", func() {
	var (
		c *idcache.IDCache
	)

	BeforeEach(func() {
		_, js, _, err := helpers.NewInProcessNATSServer()
		Expect(err).ToNot(HaveOccurred())

		conf := cache.Config{
			Database: "test-id-cache",
		}
		kv, err := cache.NewNatsKeyValueFromJetStream(conf, js)
		Expect(err).ToNot(HaveOccurred())

		c, err = idcache.NewStoreIDCache(kv)
		Expect(err).ToNot(HaveOccurred())

		Expect(c.Set(context.TODO(), "spaceID", "nodeID", "path")).To(Succeed())
	})

	Describe("StoreIdcache", func() {
		Describe("NewStoreIDCache", func() {
			It("should return a new StoreIDCache", func() {
				Expect(c).ToNot(BeNil())
			})
		})

		Describe("Delete", func() {
			It("should delete an entry from the cache", func() {
				v, ok := c.Get(context.TODO(), "spaceID", "nodeID")
				Expect(ok).To(BeTrue())
				Expect(v).To(Equal("path"))

				err := c.Delete(context.TODO(), "spaceID", "nodeID")
				Expect(err).ToNot(HaveOccurred())

				_, ok = c.Get(context.TODO(), "spaceID", "nodeID")
				Expect(ok).To(BeFalse())
			})
		})

		Describe("DeleteByPath", func() {
			It("should delete an entry from the cache", func() {
				v, ok := c.Get(context.TODO(), "spaceID", "nodeID")
				Expect(ok).To(BeTrue())
				Expect(v).To(Equal("path"))

				err := c.DeleteByPath(context.TODO(), "path")
				Expect(err).ToNot(HaveOccurred())

				_, ok = c.Get(context.TODO(), "spaceID", "nodeID")
				Expect(ok).To(BeFalse())
			})

			It("should not delete an entry from the cache if the path does not exist", func() {
				err := c.DeleteByPath(context.TODO(), "nonexistent")
				Expect(err).ToNot(HaveOccurred())
			})

			It("deletes recursively", func() {
				Expect(c.Set(context.TODO(), "spaceID", "nodeID", "path")).To(Succeed())
				Expect(c.Set(context.TODO(), "spaceID", "nodeID2", "path/child")).To(Succeed())
				Expect(c.Set(context.TODO(), "spaceID", "nodeID3", "path/child2")).To(Succeed())

				v, ok := c.Get(context.TODO(), "spaceID", "nodeID")
				Expect(ok).To(BeTrue())
				Expect(v).To(Equal("path"))
				v, ok = c.Get(context.TODO(), "spaceID", "nodeID2")
				Expect(ok).To(BeTrue())
				Expect(v).To(Equal("path/child"))
				v, ok = c.Get(context.TODO(), "spaceID", "nodeID3")
				Expect(ok).To(BeTrue())
				Expect(v).To(Equal("path/child2"))

				err := c.DeleteByPath(context.TODO(), "path")
				Expect(err).ToNot(HaveOccurred())

				_, ok = c.Get(context.TODO(), "spaceID", "nodeID")
				Expect(ok).To(BeFalse())
				_, ok = c.Get(context.TODO(), "spaceID", "nodeID2")
				Expect(ok).To(BeFalse())
				_, ok = c.Get(context.TODO(), "spaceID", "nodeID3")
				Expect(ok).To(BeFalse())
			})
		})
	})
})
