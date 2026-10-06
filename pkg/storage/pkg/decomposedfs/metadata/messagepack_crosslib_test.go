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

package metadata_test

import (
	"bytes"
	"testing"

	shamaton "github.com/shamaton/msgpack/v2"
	vmihailenco "github.com/vmihailenco/msgpack/v5"
)

func crossLibFixture() map[string][]byte {
	allBytes := make([]byte, 256)
	for i := range allBytes {
		allBytes[i] = byte(i)
	}

	return map[string][]byte{
		"user.foreign.type":              []byte("1"),
		"user.foreign.id":                []byte("4c510ada-c86b-4815-8820-42cdf82c3d51"),
		"user.foreign.parentid":          []byte("2a3b4c5d-0000-1111-2222-333344445555"),
		"user.foreign.name":              []byte("Ünïcödé filename 日本語.txt"),
		"user.foreign.blobid":            []byte("b1a2c3d4-e5f6-7890-abcd-ef1234567890"),
		"user.foreign.blobsize":          []byte("1048576"),
		"user.foreign.treesize":          []byte("9223372036854775807"), // max int64 as string
		"user.foreign.propagation":       []byte("1"),
		"user.foreign.cs.sha1":           []byte("da39a3ee5e6b4b0d3255bfef95601890afd80709"),
		"user.foreign.grant.u:some-uuid": allBytes, // binary ACE-like value, full byte range
		"user.foreign.md.arbitrary":      {0x00, 0xff, 0x10, 0x80, 0x7f},
		"user.foreign.empty":             []byte(""), // empty value
		"user.foreign.nilish":            nil,        // nil value
		"user.foreign.space.description": []byte("A\nmulti-line\tvalue with control chars \x01\x02"),
	}
}

func equalAttribs(t *testing.T, want, got map[string][]byte) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("key count mismatch: want %d keys, got %d", len(want), len(got))
	}
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			t.Fatalf("missing key after round-trip: %q", k)
		}
		if !bytes.Equal(wv, gv) {
			t.Fatalf("value mismatch for key %q:\n want %v\n  got %v", k, wv, gv)
		}
	}
}

func TestMsgpackShamatonToVmihailenco(t *testing.T) {
	in := crossLibFixture()

	blob, err := shamaton.Marshal(in)
	if err != nil {
		t.Fatalf("shamaton.Marshal failed: %v", err)
	}

	out := map[string][]byte{}
	if err := vmihailenco.Unmarshal(blob, &out); err != nil {
		t.Fatalf("vmihailenco.Unmarshal of shamaton-written data failed: %v", err)
	}

	equalAttribs(t, in, out)
}

func TestMsgpackVmihailencoToShamaton(t *testing.T) {
	in := crossLibFixture()

	blob, err := vmihailenco.Marshal(in)
	if err != nil {
		t.Fatalf("vmihailenco.Marshal failed: %v", err)
	}

	out := map[string][]byte{}
	if err := shamaton.Unmarshal(blob, &out); err != nil {
		t.Fatalf("shamaton.Unmarshal of vmihailenco-written data failed: %v", err)
	}

	equalAttribs(t, in, out)
}

func TestMsgpackSpaceIndexCrossLibrary(t *testing.T) {
	in := map[string]string{
		"4c510ada-c86b-4815-8820-42cdf82c3d51": "../../../spaces/4c/510ada-c86b-4815-8820-42cdf82c3d51/nodes/4c/51/0a/da/-c86b-4815-8820-42cdf82c3d51",
		"2a3b4c5d-0000-1111-2222-333344445555": "../../../spaces/2a/3b4c5d-0000-1111-2222-333344445555/nodes/2a/3b/4c/5d/-0000-1111-2222-333344445555",
	}

	blob, err := shamaton.Marshal(in)
	if err != nil {
		t.Fatalf("shamaton.Marshal (index) failed: %v", err)
	}
	out := map[string]string{}
	if err := vmihailenco.Unmarshal(blob, &out); err != nil {
		t.Fatalf("vmihailenco.Unmarshal of shamaton-written index failed: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("index key count mismatch: want %d, got %d", len(in), len(out))
	}
	for k, wv := range in {
		if out[k] != wv {
			t.Fatalf("index value mismatch for %q: want %q got %q", k, wv, out[k])
		}
	}
}
