package cache

import (
	"testing"
	"time"

	"github.com/opencloud-eu/reva/v2/pkg/store"
)

func TestIsSharedMetadataStore(t *testing.T) {
	for _, storeType := range []string{store.TypeEtcd, store.TypeRedis, store.TypeRedisSentinel, store.TypeNatsJS, store.TypeNatsJSKV} {
		if !isSharedMetadataStore(storeType) {
			t.Errorf("store %q classified as process-local", storeType)
		}
	}
	for _, storeType := range []string{store.TypeMemory, "mem", "", store.TypeOCMem, store.TypeNoop, "unknown"} {
		if isSharedMetadataStore(storeType) {
			t.Errorf("store %q classified as shared", storeType)
		}
	}
}

func TestInvalidate(t *testing.T) {
	for _, shared := range []bool{false, true} {
		c := NewFileMetadataCache(Config{Store: "memory", Database: "test", Table: "filemetadata", TTL: time.Minute}).(*fileMetadataCache)
		c.shared = shared
		if err := c.PushToCache("space/node", map[string]string{"k": "v"}); err != nil {
			t.Fatal(err)
		}
		if err := c.Invalidate("space/node"); err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		err := c.PullFromCache("space/node", &got)
		if kept := err == nil; kept != shared {
			t.Errorf("shared=%v: entry kept=%v, want %v", shared, kept, shared)
		}
		if err := c.RemoveMetadata("space/node"); err != nil {
			t.Fatal(err)
		}
		if err := c.PullFromCache("space/node", &got); err == nil {
			t.Errorf("shared=%v: explicit removal kept the entry", shared)
		}
	}
}
