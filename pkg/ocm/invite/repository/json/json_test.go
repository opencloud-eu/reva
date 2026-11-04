// Copyright 2018-2023 CERN
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

package json

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	invitepb "github.com/cs3org/go-cs3apis/cs3/ocm/invite/v1beta1"
	typespb "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/ocm/invite"
	"github.com/stretchr/testify/assert"
)

func setupTestManager(t *testing.T) *manager {
	// Create a temporary file for the test
	tmpDir, err := os.MkdirTemp("", "invite-manager-test")
	if err != nil {
		t.Fatalf("error creating temp file: %v", err)
	}

	// Initialize a new manager with the temp file
	config := map[string]interface{}{
		"file": filepath.Join(tmpDir, "ocm-invites.json"),
	}
	mgr, err := New(config)
	if err != nil {
		t.Fatalf("error initializing invite manager: %v", err)
	}

	return mgr.(*manager)
}

func TestAddToken(t *testing.T) {
	mgr := setupTestManager(t)
	ctx := context.Background()

	// Test data
	token := &invitepb.InviteToken{
		Token: "test-token",
		UserId: &userpb.UserId{
			OpaqueId: "user1",
		},
		Expiration: &typespb.Timestamp{
			Seconds: uint64(time.Now().Add(24 * time.Hour).Unix()),
		},
	}

	// Add token
	err := mgr.AddToken(ctx, token)
	assert.NoError(t, err)

	// Check if the token was added correctly
	storedToken, err := mgr.GetToken(ctx, token.Token)
	assert.NoError(t, err)
	assert.Equal(t, token, storedToken)
}

func TestGetToken_NotFound(t *testing.T) {
	mgr := setupTestManager(t)
	ctx := context.Background()

	// Try to get a non-existent token
	_, err := mgr.GetToken(ctx, "non-existent-token")
	assert.ErrorIs(t, err, invite.ErrTokenNotFound)
}

func TestListTokens(t *testing.T) {
	mgr := setupTestManager(t)
	ctx := context.Background()

	initiator := &userpb.UserId{
		OpaqueId: "user1",
	}

	// Add some tokens
	token1 := &invitepb.InviteToken{
		Token:  "token1",
		UserId: initiator,
	}
	token2 := &invitepb.InviteToken{
		Token:  "token2",
		UserId: initiator,
	}

	_ = mgr.AddToken(ctx, token1)
	_ = mgr.AddToken(ctx, token2)

	// List tokens
	tokens, err := mgr.ListTokens(ctx, initiator)
	assert.NoError(t, err)
	assert.Len(t, tokens, 2)
}

func TestAddRemoteUser(t *testing.T) {
	mgr := setupTestManager(t)
	ctx := context.Background()

	initiator := &userpb.UserId{
		OpaqueId: "user1",
	}

	remoteUser := &userpb.User{
		Id: &userpb.UserId{
			OpaqueId: "remoteUser1",
		},
	}

	// Add remote user
	err := mgr.AddRemoteUser(ctx, initiator, remoteUser)
	assert.NoError(t, err)

	// Retrieve remote user and verify
	storedUser, err := mgr.GetRemoteUser(ctx, initiator, remoteUser.Id)
	assert.NoError(t, err)
	assert.Equal(t, remoteUser, storedUser)
}

func TestGetRemoteUser_NotFound(t *testing.T) {
	mgr := setupTestManager(t)
	ctx := context.Background()

	initiator := &userpb.UserId{
		OpaqueId: "user1",
	}

	// Try to get a non-existent remote user
	_, err := mgr.GetRemoteUser(ctx, initiator, &userpb.UserId{OpaqueId: "non-existent"})
	assert.Error(t, err)
	var notFound errtypes.NotFound
	assert.ErrorAs(t, err, &notFound)
}

func TestGetRemoteUser_LegacyAndCanonicalLookup(t *testing.T) {
	const (
		uuid = "056fc874-dd7f-11ef-ba84-af6fca4b7289"
		idp  = "cloud2.opencloud.test:10200"
	)
	legacyOpaque := uuid + "@https://" + idp
	ctx := context.Background()
	initiator := &userpb.UserId{OpaqueId: "alan"}

	t.Run("legacy stored found by canonical query", func(t *testing.T) {
		mgr := setupTestManager(t)
		mgr.model.AcceptedUsers[initiator.OpaqueId] = []*userpb.User{{
			Id:          &userpb.UserId{OpaqueId: legacyOpaque, Idp: idp},
			DisplayName: "Mary",
		}}
		user, err := mgr.GetRemoteUser(ctx, initiator, &userpb.UserId{OpaqueId: uuid, Idp: idp})
		assert.NoError(t, err)
		assert.Equal(t, legacyOpaque, user.Id.OpaqueId)
	})

	t.Run("legacy stored found by legacy graph query opaque", func(t *testing.T) {
		mgr := setupTestManager(t)
		mgr.model.AcceptedUsers[initiator.OpaqueId] = []*userpb.User{{
			Id:          &userpb.UserId{OpaqueId: legacyOpaque, Idp: idp},
			DisplayName: "Mary",
		}}
		user, err := mgr.GetRemoteUser(ctx, initiator, &userpb.UserId{OpaqueId: legacyOpaque, Idp: idp})
		assert.NoError(t, err)
		assert.Equal(t, legacyOpaque, user.Id.OpaqueId)
	})

	t.Run("canonical stored found by qualified query", func(t *testing.T) {
		mgr := setupTestManager(t)
		mgr.model.AcceptedUsers[initiator.OpaqueId] = []*userpb.User{{
			Id:          &userpb.UserId{OpaqueId: uuid, Idp: idp},
			DisplayName: "Mary",
		}}
		user, err := mgr.GetRemoteUser(ctx, initiator, &userpb.UserId{OpaqueId: uuid + "@" + idp, Idp: idp})
		assert.NoError(t, err)
		assert.Equal(t, uuid, user.Id.OpaqueId)
	})
}

func TestAddRemoteUser_LegacyDedup(t *testing.T) {
	const (
		uuid = "056fc874-dd7f-11ef-ba84-af6fca4b7289"
		idp  = "cloud2.opencloud.test:10200"
	)
	legacyOpaque := uuid + "@https://" + idp
	ctx := context.Background()
	initiator := &userpb.UserId{OpaqueId: "alan"}

	mgr := setupTestManager(t)
	mgr.model.AcceptedUsers[initiator.OpaqueId] = []*userpb.User{{
		Id:          &userpb.UserId{OpaqueId: legacyOpaque, Idp: idp},
		DisplayName: "Mary",
	}}

	canonicalUser := &userpb.User{
		Id: &userpb.UserId{
			OpaqueId: uuid,
			Idp:      idp,
		},
		DisplayName: "Mary",
	}

	err := mgr.AddRemoteUser(ctx, initiator, canonicalUser)
	assert.ErrorIs(t, err, invite.ErrUserAlreadyAccepted)
	assert.Len(t, mgr.model.AcceptedUsers[initiator.OpaqueId], 1)
	assert.Equal(t, legacyOpaque, mgr.model.AcceptedUsers[initiator.OpaqueId][0].Id.OpaqueId)
}

func TestDeleteRemoteUser_LegacyFallback(t *testing.T) {
	const (
		uuid = "056fc874-dd7f-11ef-ba84-af6fca4b7289"
		idp  = "cloud2.opencloud.test:10200"
	)
	legacyOpaque := uuid + "@https://" + idp
	ctx := context.Background()
	initiator := &userpb.UserId{OpaqueId: "alan"}

	mgr := setupTestManager(t)
	mgr.model.AcceptedUsers[initiator.OpaqueId] = []*userpb.User{{
		Id:          &userpb.UserId{OpaqueId: legacyOpaque, Idp: idp},
		DisplayName: "Mary",
	}}

	err := mgr.DeleteRemoteUser(ctx, initiator, &userpb.UserId{OpaqueId: uuid, Idp: idp})
	assert.NoError(t, err)
	assert.Empty(t, mgr.model.AcceptedUsers[initiator.OpaqueId])

	_, err = mgr.GetRemoteUser(ctx, initiator, &userpb.UserId{OpaqueId: legacyOpaque, Idp: idp})
	assert.Error(t, err)
	var notFound errtypes.NotFound
	assert.ErrorAs(t, err, &notFound)
}

func TestDeleteRemoteUser(t *testing.T) {
	mgr := setupTestManager(t)
	ctx := context.Background()

	initiator := &userpb.UserId{
		OpaqueId: "user1",
	}

	remoteUser1 := &userpb.User{
		Id: &userpb.UserId{
			OpaqueId: "remoteUser1",
		},
	}
	remoteUser2 := &userpb.User{
		Id: &userpb.UserId{
			OpaqueId: "remoteUser2",
		},
	}
	remoteUser3 := &userpb.User{
		Id: &userpb.UserId{
			OpaqueId: "remoteUser3",
		},
	}

	// Add remote users
	err := mgr.AddRemoteUser(ctx, initiator, remoteUser1)
	assert.NoError(t, err)
	err = mgr.AddRemoteUser(ctx, initiator, remoteUser2)
	assert.NoError(t, err)
	err = mgr.AddRemoteUser(ctx, initiator, remoteUser3)
	assert.NoError(t, err)

	// Delete remote user
	err = mgr.DeleteRemoteUser(ctx, initiator, remoteUser2.Id)
	assert.NoError(t, err)

	// Try to get the deleted user
	_, err = mgr.GetRemoteUser(ctx, initiator, remoteUser1.Id)
	assert.NoError(t, err)
	_, err = mgr.GetRemoteUser(ctx, initiator, remoteUser2.Id)
	assert.Error(t, err)
	_, err = mgr.GetRemoteUser(ctx, initiator, remoteUser3.Id)
	assert.NoError(t, err)
}
