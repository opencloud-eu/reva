package metadata_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencloud-eu/reva/v2/pkg/storage/cache"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/metadata/prefixes"
)

func TestGetCached(t *testing.T) {
	for _, name := range []string{"xattrs", "hybrid", "messagepack"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			n := &testNode{spaceID: "space", id: "node", path: filepath.Join(dir, "file")}
			if err := os.WriteFile(n.path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			cfg := cache.Config{Store: "memory", Database: dir}
			var backend metadata.Backend
			switch name {
			case "xattrs":
				backend = metadata.NewXattrsBackend(cfg)
			case "hybrid":
				backend = metadata.NewHybridBackend(1000, func(n metadata.MetadataNode) string {
					return n.InternalPath() + ".mpk"
				}, cfg)
			case "messagepack":
				backend = metadata.NewMessagePackBackend(cfg)
			}
			key := prefixes.MTimeAttr
			if err := backend.Set(ctx, n, key, []byte("disk value")); err != nil {
				t.Fatal(err)
			}
			if _, err := backend.All(ctx, n); err != nil {
				t.Fatal(err)
			}
			if val, ok := backend.GetCached(ctx, n, key); !ok || string(val) != "disk value" {
				t.Fatalf("cached value = %q, hit = %v", val, ok)
			}
			if _, ok := backend.GetCached(ctx, n, "missing"); ok {
				t.Fatal("missing attribute reported as a cache hit")
			}
			if err := backend.InvalidateCache(ctx, n); err != nil {
				t.Fatal(err)
			}
			if val, ok := backend.GetCached(ctx, n, key); ok {
				t.Fatalf("cache miss fell back to disk: %q", val)
			}
			if val, err := backend.Get(ctx, n, key); err != nil || string(val) != "disk value" {
				t.Fatalf("disk value = %q, error = %v", val, err)
			}
		})
	}
}
