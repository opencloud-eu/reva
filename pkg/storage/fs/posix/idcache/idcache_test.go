package idcache_test

import (
	"context"
	"errors"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/opencloud-eu/reva/v3/pkg/errtypes"
	"github.com/opencloud-eu/reva/v3/pkg/storage/cache"
	"github.com/opencloud-eu/reva/v3/pkg/storage/fs/posix/idcache"
	helpers "github.com/opencloud-eu/reva/v3/pkg/storage/fs/posix/testhelpers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type mockKV struct {
	jetstream.KeyValue
	failCount int
	calls     map[string]int
}

func newMockKV(kv jetstream.KeyValue, failCount int) *mockKV {
	return &mockKV{
		KeyValue:  kv,
		failCount: failCount,
		calls:     make(map[string]int),
	}
}

func (m *mockKV) Get(ctx context.Context, key string) (jetstream.KeyValueEntry, error) {
	m.calls["Get"]++
	if m.calls["Get"] <= m.failCount {
		return nil, errors.New("mock transient error")
	}
	return m.KeyValue.Get(ctx, key)
}

func (m *mockKV) Put(ctx context.Context, key string, value []byte) (uint64, error) {
	m.calls["Put"]++
	if m.calls["Put"] <= m.failCount {
		return 0, errors.New("mock transient error")
	}
	return m.KeyValue.Put(ctx, key, value)
}

func (m *mockKV) Purge(ctx context.Context, key string, opts ...jetstream.KVDeleteOpt) error {
	m.calls["Purge"]++
	if m.calls["Purge"] <= m.failCount {
		return errors.New("mock transient error")
	}
	return m.KeyValue.Purge(ctx, key, opts...)
}

func (m *mockKV) Watch(ctx context.Context, keys string, opts ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	m.calls["Watch"]++
	if m.calls["Watch"] <= m.failCount {
		return nil, errors.New("mock transient error")
	}
	return m.KeyValue.Watch(ctx, keys, opts...)
}

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
				v, err := c.Get(context.TODO(), "spaceID", "nodeID")
				Expect(err).ToNot(HaveOccurred())
				Expect(v).To(Equal("path"))

				err = c.Delete(context.TODO(), "spaceID", "nodeID")
				Expect(err).ToNot(HaveOccurred())

				_, err = c.Get(context.TODO(), "spaceID", "nodeID")
				Expect(err).To(HaveOccurred())
			})
		})

		Describe("DeleteByPath", func() {
			It("should delete an entry from the cache", func() {
				v, err := c.Get(context.TODO(), "spaceID", "nodeID")
				Expect(err).ToNot(HaveOccurred())
				Expect(v).To(Equal("path"))

				err = c.DeleteByPath(context.TODO(), "path")
				Expect(err).ToNot(HaveOccurred())

				_, err = c.Get(context.TODO(), "spaceID", "nodeID")
				Expect(err).To(HaveOccurred())
			})

			It("should not delete an entry from the cache if the path does not exist", func() {
				err := c.DeleteByPath(context.TODO(), "nonexistent")
				Expect(err).ToNot(HaveOccurred())
			})

			It("deletes recursively", func() {
				Expect(c.Set(context.TODO(), "spaceID", "nodeID", "path")).To(Succeed())
				Expect(c.Set(context.TODO(), "spaceID", "nodeID2", "path/child")).To(Succeed())
				Expect(c.Set(context.TODO(), "spaceID", "nodeID3", "path/child2")).To(Succeed())

				v, err := c.Get(context.TODO(), "spaceID", "nodeID")
				Expect(err).ToNot(HaveOccurred())
				Expect(v).To(Equal("path"))
				v, err = c.Get(context.TODO(), "spaceID", "nodeID2")
				Expect(err).ToNot(HaveOccurred())
				Expect(v).To(Equal("path/child"))
				v, err = c.Get(context.TODO(), "spaceID", "nodeID3")
				Expect(err).ToNot(HaveOccurred())
				Expect(v).To(Equal("path/child2"))

				err = c.DeleteByPath(context.TODO(), "path")
				Expect(err).ToNot(HaveOccurred())

				_, err = c.Get(context.TODO(), "spaceID", "nodeID")
				Expect(err).To(HaveOccurred())
				_, err = c.Get(context.TODO(), "spaceID", "nodeID2")
				Expect(err).To(HaveOccurred())
				_, err = c.Get(context.TODO(), "spaceID", "nodeID3")
				Expect(err).To(HaveOccurred())
			})
		})

		Describe("Claim", func() {
			It("caches a path that isn't cached yet", func() {
				Expect(c.Claim(context.TODO(), "spaceID", "newID", "newpath")).To(Succeed())

				v, err := c.Get(context.TODO(), "spaceID", "newID")
				Expect(err).ToNot(HaveOccurred())
				Expect(v).To(Equal("newpath"))
				_, id, err := c.GetByPath(context.TODO(), "newpath")
				Expect(err).ToNot(HaveOccurred())
				Expect(id).To(Equal("newID"))
			})

			It("leaves a cached path alone", func() {
				err := c.Claim(context.TODO(), "spaceID", "newID", "path")
				Expect(err).To(BeAssignableToTypeOf(errtypes.AlreadyExists("")))

				_, id, err := c.GetByPath(context.TODO(), "path")
				Expect(err).ToNot(HaveOccurred())
				Expect(id).To(Equal("nodeID"))
				_, err = c.Get(context.TODO(), "spaceID", "newID")
				Expect(err).To(HaveOccurred())
			})

			It("caches a path whose entry was deleted", func() {
				Expect(c.Delete(context.TODO(), "spaceID", "nodeID")).To(Succeed())
				Expect(c.Claim(context.TODO(), "spaceID", "newID", "path")).To(Succeed())

				_, id, err := c.GetByPath(context.TODO(), "path")
				Expect(err).ToNot(HaveOccurred())
				Expect(id).To(Equal("newID"))
			})
		})

		Describe("Release", func() {
			It("removes both entries of a claimed path", func() {
				Expect(c.Claim(context.TODO(), "spaceID", "newID", "newpath")).To(Succeed())
				Expect(c.Release(context.TODO(), "spaceID", "newID", "newpath")).To(Succeed())

				_, err := c.Get(context.TODO(), "spaceID", "newID")
				Expect(err).To(HaveOccurred())
				_, _, err = c.GetByPath(context.TODO(), "newpath")
				Expect(err).To(HaveOccurred())
			})

			It("keeps the path entry of another node", func() {
				Expect(c.Release(context.TODO(), "spaceID", "newID", "path")).To(Succeed())

				_, id, err := c.GetByPath(context.TODO(), "path")
				Expect(err).ToNot(HaveOccurred())
				Expect(id).To(Equal("nodeID"))
				v, err := c.Get(context.TODO(), "spaceID", "nodeID")
				Expect(err).ToNot(HaveOccurred())
				Expect(v).To(Equal("path"))
			})
		})

		Describe("Retries", func() {
			It("should retry operations and succeed if transient errors resolve", func() {
				_, js, _, err := helpers.NewInProcessNATSServer()
				Expect(err).ToNot(HaveOccurred())

				conf := cache.Config{Database: "test-id-cache-retry"}
				origKV, err := cache.NewNatsKeyValueFromJetStream(conf, js)
				Expect(err).ToNot(HaveOccurred())

				// configure it to fail 2 times
				mkv := newMockKV(origKV, 2)
				retryCache, err := idcache.NewStoreIDCache(mkv)
				Expect(err).ToNot(HaveOccurred())

				// Set triggers Put (which fails twice, then succeeds)
				err = retryCache.Set(context.TODO(), "spaceIDRetry", "nodeIDRetry", "pathRetry")
				Expect(err).ToNot(HaveOccurred())
				Expect(mkv.calls["Put"]).To(Equal(4)) // 2 times for first key,

				// Get triggers Get
				val, err := retryCache.Get(context.TODO(), "spaceIDRetry", "nodeIDRetry")
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("pathRetry"))
				Expect(mkv.calls["Get"]).To(BeNumerically(">", 2)) // 2 failed, on succeeded

				// Delete triggers Get and Purge
				err = retryCache.Delete(context.TODO(), "spaceIDRetry", "nodeIDRetry")
				Expect(err).ToNot(HaveOccurred())
				Expect(mkv.calls["Purge"]).To(BeNumerically(">", 2)) // 2 times for first key, 2 times for reverse key
			})

			It("should fail if transient errors persist", func() {
				_, js, _, err := helpers.NewInProcessNATSServer()
				Expect(err).ToNot(HaveOccurred())

				conf := cache.Config{Database: "test-id-cache-retry-fail"}
				origKV, err := cache.NewNatsKeyValueFromJetStream(conf, js)
				Expect(err).ToNot(HaveOccurred())

				// configure it to fail 10 times (more than 5 retries)
				mkv := newMockKV(origKV, 10)
				retryCache, err := idcache.NewStoreIDCache(mkv)
				Expect(err).ToNot(HaveOccurred())

				err = retryCache.Set(context.TODO(), "spaceIDRetry", "nodeIDRetry", "pathRetry")
				Expect(err).To(HaveOccurred())
			})
		})

	})
})
