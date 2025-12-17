// Copyright 2018-2021 CERN
// Copyright 2025 OpenCloud GmbH <mail@opencloud.eu>
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

package tree

import (
	"errors"
	"testing"

	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/options"
	decomposedoptions "github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/options"
)

var (
	ErrAny = errors.New("any error")
)

// test tree.findSpaceId isolated, without setting up a full Tree structure
// ginkgo.SynchronizedBeforeSuite is not needed and would fail on systems without inotify support.
func TestTree_findSpaceId(t *testing.T) {
	tests := []struct {
		name string
		path string
		tree *Tree
		err  error
	}{
		{
			name: "tree.options.root >> root reached without finding space id",
			path: "/foo/bar/baz",
			err:  ErrRootReached,
			tree: &Tree{
				options: &options.Options{
					Options: decomposedoptions.Options{
						Root: "/foo/bar/baz",
					},
				},
			},
		},
		{
			name: "tree.personalSpacesRoot >> root reached without finding space id",
			path: "/foo/bar/baz",
			err:  ErrRootReached,
			tree: &Tree{
				personalSpacesRoot: "/foo/bar/baz",
				options: &options.Options{
					Options: decomposedoptions.Options{},
				},
			},
		},
		{
			name: "tree.projectSpacesRoot >> root reached without finding space id",
			path: "/foo/bar/baz",
			err:  ErrRootReached,
			tree: &Tree{
				projectSpacesRoot: "/foo/bar/baz",
				options: &options.Options{
					Options: decomposedoptions.Options{},
				},
			},
		},
		{
			name: "no root found",
			path: t.TempDir(),
			err:  ErrAny,
			tree: &Tree{
				options: &options.Options{
					Options: decomposedoptions.Options{
						Root: "/foo/bar/baz",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.tree.findSpaceId(tt.path)
			switch {
			case errors.Is(tt.err, ErrAny) && err != nil:
				break
			case !errors.Is(err, tt.err):
				t.Errorf("Tree.findSpaceId() error = %v, wantErr %v", err, tt.err)
			}
		})
	}
}
