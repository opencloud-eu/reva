// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"testing"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
)

func newTestFileMetadataCache() FileMetadataCache {
	return NewFileMetadataCache(Config{Store: "memory", Database: "test", Table: "test"})
}

func TestPullFromCacheReturnsWhatWasPushed(t *testing.T) {
	c := newTestFileMetadataCache()
	if err := c.PushToCache("key", map[string][]byte{"user.a": []byte("1")}); err != nil {
		t.Fatalf("PushToCache: %v", err)
	}

	attribs := map[string][]byte{}
	if err := c.PullFromCache("key", &attribs); err != nil {
		t.Fatalf("PullFromCache: %v", err)
	}
	if string(attribs["user.a"]) != "1" || len(attribs) != 1 {
		t.Fatalf("PullFromCache: got %v", attribs)
	}
}

// A nil map was cached by the hybrid metadata backend after a failed read,
// https://github.com/opencloud-eu/reva/issues/1073: reading it back panicked.
func TestPullFromCacheTakesANilValueAsNotFound(t *testing.T) {
	c := newTestFileMetadataCache()
	var nilMap map[string][]byte
	if err := c.PushToCache("key", nilMap); err != nil {
		t.Fatalf("PushToCache: %v", err)
	}

	attribs := map[string][]byte{"left": []byte("alone")}
	if err := c.PullFromCache("key", &attribs); err == nil {
		t.Fatal("PullFromCache of a nil value: no error, want not found")
	}
	if attribs == nil || string(attribs["left"]) != "alone" {
		t.Fatalf("PullFromCache of a nil value changed the destination: %v", attribs)
	}
}

func TestPullFromCacheDecodesIntoAPointerToAStruct(t *testing.T) {
	c := newTestFileMetadataCache()
	if err := c.PushToCache("key", &provider.StatResponse{Info: &provider.ResourceInfo{Path: "/a"}}); err != nil {
		t.Fatalf("PushToCache: %v", err)
	}

	s := &provider.StatResponse{}
	if err := c.PullFromCache("key", s); err != nil {
		t.Fatalf("PullFromCache: %v", err)
	}
	if s.GetInfo().GetPath() != "/a" {
		t.Fatalf("PullFromCache: got %v", s)
	}
}

func TestPullFromCacheOfAMissingKey(t *testing.T) {
	c := newTestFileMetadataCache()
	attribs := map[string][]byte{}
	if err := c.PullFromCache("missing", &attribs); err == nil {
		t.Fatal("PullFromCache of a missing key: no error")
	}
}
