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

package metadata

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	shamaton "github.com/shamaton/msgpack/v2"
	vmihailenco "github.com/vmihailenco/msgpack/v5"

	"github.com/opencloud-eu/reva/v2/pkg/storage/cache"
)

type compatTestNode struct {
	spaceID  string
	id       string
	path     string
	lockHeld bool
}

func (t *compatTestNode) GetSpaceID() string    { return t.spaceID }
func (t *compatTestNode) GetID() string         { return t.id }
func (t *compatTestNode) InternalPath() string  { return t.path }
func (t *compatTestNode) LockHeld() bool        { return t.lockHeld }
func (t *compatTestNode) SetLockHeld(held bool) { t.lockHeld = held }

func newInner(t *testing.T) (MessagePackBackend, *compatTestNode) {
	t.Helper()
	dir := t.TempDir()
	inner := NewMessagePackBackend(cache.Config{Database: dir})
	n := &compatTestNode{spaceID: "space-1", id: "node-1", path: filepath.Join(dir, "node")}
	if err := os.WriteFile(n.path, []byte{}, 0600); err != nil {
		t.Fatalf("create node file: %v", err)
	}
	return inner, n
}

func seedLegacy(t *testing.T, n *compatTestNode, attribs map[string][]byte) {
	t.Helper()
	blob, err := shamaton.Marshal(attribs)
	if err != nil {
		t.Fatalf("seed marshal: %v", err)
	}
	if err := os.WriteFile(n.path+".mpk", blob, 0600); err != nil {
		t.Fatalf("seed write: %v", err)
	}
}

func readRawDisk(t *testing.T, n *compatTestNode) map[string][]byte {
	t.Helper()
	blob, err := os.ReadFile(n.path + ".mpk")
	if err != nil {
		t.Fatalf("read raw disk: %v", err)
	}
	out := map[string][]byte{}
	if len(blob) > 0 {
		if err := vmihailenco.Unmarshal(blob, &out); err != nil {
			t.Fatalf("unmarshal raw disk: %v", err)
		}
	}
	return out
}

func keysWithPrefix(m map[string][]byte, prefix string) []string {
	var ks []string
	for k := range m {
		if strings.HasPrefix(k, prefix) {
			ks = append(ks, k)
		}
	}
	sort.Strings(ks)
	return ks
}

var legacySeed = map[string][]byte{
	"user.foreign.id":       []byte("node-1"),
	"user.foreign.type":     []byte("1"),
	"user.foreign.name":     []byte("hello.txt"),
	"user.foreign.blobid":   []byte("blob-abc"),
	"user.foreign.treesize": []byte("42"),
}

func TestTranslateRenamesLegacyKeys(t *testing.T) {
	in := map[string][]byte{"user.foreign.id": []byte("x"), "unprefixed": []byte("y")}
	out := translate(in, "user.foreign.", "user.oc.")
	if string(out["user.oc.id"]) != "x" {
		t.Fatalf("expected user.oc.id=x, got %q", out["user.oc.id"])
	}
	if string(out["unprefixed"]) != "y" {
		t.Fatalf("unprefixed key should pass through")
	}
	if _, ok := in["user.oc.id"]; ok {
		t.Fatalf("translate mutated its input")
	}
}

func TestTranslateNewKeyWins(t *testing.T) {
	in := map[string][]byte{"user.foreign.id": []byte("old"), "user.oc.id": []byte("new")}
	if string(translate(in, "user.foreign.", "user.oc.")["user.oc.id"]) != "new" {
		t.Fatalf("new key must win")
	}
}

func TestTranslateBackRenamesAndDiskWins(t *testing.T) {
	out := translate(map[string][]byte{"user.oc.name": []byte("n"), "unprefixed": []byte("u")}, "user.oc.", "user.foreign.")
	if string(out["user.foreign.name"]) != "n" {
		t.Fatalf("expected user.foreign.name=n, got %q", out["user.foreign.name"])
	}
	if string(out["unprefixed"]) != "u" {
		t.Fatalf("unprefixed key should pass through")
	}
	both := translate(map[string][]byte{"user.foreign.id": []byte("legacy"), "user.oc.id": []byte("new")}, "user.oc.", "user.foreign.")
	if string(both["user.foreign.id"]) != "legacy" {
		t.Fatalf("legacy key must win, got %q", both["user.foreign.id"])
	}
}

func TestReadTranslatesToNativeKeys(t *testing.T) {
	inner, n := newInner(t)
	seedLegacy(t, n, legacySeed)
	b := inner.WithPrefix("user.foreign.")

	all, err := b.All(context.Background(), n)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(keysWithPrefix(all, "user.foreign.")) != 0 {
		t.Fatalf("All returned foreign keys: %v", keysWithPrefix(all, "user.foreign."))
	}
	if string(all["user.oc.id"]) != "node-1" || string(all["user.oc.name"]) != "hello.txt" {
		t.Fatalf("translated values wrong: %v", all)
	}

	v, err := b.Get(context.Background(), n, "user.oc.blobid")
	if err != nil || string(v) != "blob-abc" {
		t.Fatalf("Get user.oc.blobid: v=%q err=%v", v, err)
	}
	i, err := b.GetInt64(context.Background(), n, "user.oc.treesize")
	if err != nil || i != 42 {
		t.Fatalf("GetInt64 user.oc.treesize: i=%d err=%v", i, err)
	}

	_, err = b.Get(context.Background(), n, "user.oc.doesnotexist")
	if err == nil || !IsAttrUnset(err) {
		t.Fatalf("expected IsAttrUnset error, got %v", err)
	}
}

func TestAllFromPathTranslates(t *testing.T) {
	inner, n := newInner(t)
	seedLegacy(t, n, legacySeed)
	b := inner.WithPrefix("user.foreign.")

	m, err := b.AllFromPath(context.Background(), n.path)
	if err != nil {
		t.Fatalf("AllFromPath: %v", err)
	}
	if len(keysWithPrefix(m, "user.foreign.")) != 0 {
		t.Fatalf("AllFromPath returned foreign keys: %v", keysWithPrefix(m, "user.foreign."))
	}
	if string(m["user.oc.blobid"]) != "blob-abc" {
		t.Fatalf("expected translated user.oc.blobid=blob-abc, got %q", m["user.oc.blobid"])
	}
}

func TestWriteKeepsForeignFormat(t *testing.T) {
	inner, n := newInner(t)
	seedLegacy(t, n, legacySeed)
	b := inner.WithPrefix("user.foreign.")

	if err := b.SetMultiple(context.Background(), n, map[string][]byte{"user.oc.name": []byte("renamed.txt")}); err != nil {
		t.Fatalf("SetMultiple: %v", err)
	}
	disk := readRawDisk(t, n)
	if got := keysWithPrefix(disk, "user.oc."); len(got) != 0 {
		t.Fatalf("disk gained OpenCloud keys: %v", got)
	}
	if string(disk["user.foreign.name"]) != "renamed.txt" {
		t.Fatalf("new value did not arrive under foreign key: %q", disk["user.foreign.name"])
	}
	if string(disk["user.foreign.blobid"]) != "blob-abc" {
		t.Fatalf("unrelated foreign key changed: %q", disk["user.foreign.blobid"])
	}

	all, err := b.All(context.Background(), n)
	if err != nil || string(all["user.oc.name"]) != "renamed.txt" {
		t.Fatalf("round trip failed: all=%v err=%v", all, err)
	}
}

func TestBrandNewNodeIsForeignFormat(t *testing.T) {
	inner, n := newInner(t) // no seed -> brand new, no .mpk yet
	b := inner.WithPrefix("user.foreign.")

	if err := b.SetMultiple(context.Background(), n, map[string][]byte{
		"user.oc.id":   []byte("node-1"),
		"user.oc.type": []byte("1"),
	}); err != nil {
		t.Fatalf("SetMultiple: %v", err)
	}
	disk := readRawDisk(t, n)
	if got := keysWithPrefix(disk, "user.oc."); len(got) != 0 {
		t.Fatalf("brand new node wrote OpenCloud keys: %v", got)
	}
	if string(disk["user.foreign.id"]) != "node-1" {
		t.Fatalf("expected foreign user.foreign.id, got %v", disk)
	}
}

func TestRemoveDeletesTheForeignKey(t *testing.T) {
	inner, n := newInner(t)
	seedLegacy(t, n, map[string][]byte{
		"user.foreign.name": []byte("keep-or-not"),
		"user.foreign.id":   []byte("node-1"),
	})
	b := inner.WithPrefix("user.foreign.")

	if err := b.Remove(context.Background(), n, "user.oc.name"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	disk := readRawDisk(t, n)
	if _, ok := disk["user.foreign.name"]; ok {
		t.Fatalf("foreign twin not removed: %v", disk)
	}
	if _, ok := disk["user.foreign.id"]; !ok {
		t.Fatalf("unrelated key removed: %v", disk)
	}
}
