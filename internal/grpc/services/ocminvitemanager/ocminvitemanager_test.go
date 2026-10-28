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

package ocminvitemanager

import (
	"context"
	"testing"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	invitepb "github.com/cs3org/go-cs3apis/cs3/ocm/invite/v1beta1"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	ctxpkg "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubInviteRepo struct {
	getRemoteUserFn func(ctx context.Context, initiator *userpb.UserId, remoteUserID *userpb.UserId) (*userpb.User, error)
}

func (s *stubInviteRepo) AddToken(context.Context, *invitepb.InviteToken) error {
	panic("not implemented")
}

func (s *stubInviteRepo) GetToken(context.Context, string) (*invitepb.InviteToken, error) {
	panic("not implemented")
}

func (s *stubInviteRepo) ListTokens(context.Context, *userpb.UserId) ([]*invitepb.InviteToken, error) {
	panic("not implemented")
}

func (s *stubInviteRepo) AddRemoteUser(context.Context, *userpb.UserId, *userpb.User) error {
	panic("not implemented")
}

func (s *stubInviteRepo) GetRemoteUser(ctx context.Context, initiator *userpb.UserId, remoteUserID *userpb.UserId) (*userpb.User, error) {
	return s.getRemoteUserFn(ctx, initiator, remoteUserID)
}

func (s *stubInviteRepo) FindRemoteUsers(context.Context, *userpb.UserId, string) ([]*userpb.User, error) {
	panic("not implemented")
}

func (s *stubInviteRepo) DeleteRemoteUser(context.Context, *userpb.UserId, *userpb.UserId) error {
	panic("not implemented")
}

func TestGetAcceptedUser_NotFound(t *testing.T) {
	const remoteID = "056fc874-dd7f-11ef-ba84-af6fca4b7289"

	svc := &service{
		repo: &stubInviteRepo{
			getRemoteUserFn: func(_ context.Context, _ *userpb.UserId, remoteUserID *userpb.UserId) (*userpb.User, error) {
				return nil, errtypes.NotFound(remoteUserID.GetOpaqueId())
			},
		},
	}

	ctx := ctxpkg.ContextSetUser(context.Background(), &userpb.User{
		Id: &userpb.UserId{OpaqueId: "alan"},
	})

	resp, err := svc.GetAcceptedUser(ctx, &invitepb.GetAcceptedUserRequest{
		RemoteUserId: &userpb.UserId{
			OpaqueId: remoteID,
			Idp:      "cloud2.opencloud.test:10200",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, rpc.Code_CODE_NOT_FOUND, resp.GetStatus().GetCode())
	assert.Nil(t, resp.GetRemoteUser())
}
