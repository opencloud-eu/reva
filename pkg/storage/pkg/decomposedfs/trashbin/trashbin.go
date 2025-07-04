// Copyright 2018-2024 CERN
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

package trashbin

import (
	"context"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
)

type Trashbin interface {
	Setup(storage.FS) error

	ListRecycle(ctx context.Context, spaceID, key, relativePath string) ([]*provider.RecycleItem, error)
	RestoreRecycleItem(ctx context.Context, spaceID, key, relativePath string, restoreRef *provider.Reference) (*node.Node, error)
	PurgeRecycleItem(ctx context.Context, spaceID, key, relativePath string) error
	EmptyRecycle(ctx context.Context, spaceID string) error
	IsEmpty(ctx context.Context, spaceID string) bool
}
