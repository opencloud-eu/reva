// Copyright 2026 OpenCloud GmbH
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

package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	grouppb "github.com/cs3org/go-cs3apis/cs3/identity/group/v1beta1"
	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	collaboration "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	registry "github.com/cs3org/go-cs3apis/cs3/storage/registry/v1beta1"
	typesv1beta1 "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/google/uuid"
	ctxpkg "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	"github.com/opencloud-eu/reva/v2/pkg/share"
	"github.com/opencloud-eu/reva/v2/pkg/share/manager/jsoncs3/shareid"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
)

type ImportSpaceMembersMigration struct {
	cfg          config
	sharesChan   chan *collaboration.Share
	receivedChan chan share.ReceivedShareWithUser
}

func init() {
	registerMigration(&ImportSpaceMembersMigration{})
}

func (m *ImportSpaceMembersMigration) Initialize(cfg config) {
	m.cfg = cfg
	m.sharesChan = make(chan *collaboration.Share)
	m.receivedChan = make(chan share.ReceivedShareWithUser)
}

func (m *ImportSpaceMembersMigration) Name() string {
	return "import_space_members"
}

func (m *ImportSpaceMembersMigration) Version() int {
	return 1
}

func (m *ImportSpaceMembersMigration) Migrate() error {
	gwc, err := m.cfg.gatewaySelector.Next()
	if err != nil {
		return err
	}

	svcCtx, err := utils.GetServiceUserContextWithContext(context.Background(), gwc, m.cfg.serviceAccountID, m.cfg.serviceAccountSecret)
	if err != nil {
		m.cfg.logger.Error().Err(err).Msg("failed to get service user context for migration")
		return err
	}
	// List all project spaces.
	listRes, err := gwc.ListStorageSpaces(svcCtx, &provider.ListStorageSpacesRequest{
		Opaque: utils.AppendPlainToOpaque(nil, "unrestricted", "true"),
		Filters: []*provider.ListStorageSpacesRequest_Filter{
			{
				Type: provider.ListStorageSpacesRequest_Filter_TYPE_SPACE_TYPE,
				Term: &provider.ListStorageSpacesRequest_Filter_SpaceType{SpaceType: "project"},
			},
		},
	})
	if err != nil {
		m.cfg.logger.Error().Err(err).Msg("space-membership migration: failed to list storage spaces")
		return err
	}

	if listRes.GetStatus().GetCode() != rpc.Code_CODE_OK {
		m.cfg.logger.Error().Str("status", listRes.GetStatus().GetMessage()).Msg("space-membership migration: ListStorageSpaces returned non-OK status")
		return errtypes.InternalError("ListStorageSpaces")
	}

	spaces := listRes.GetStorageSpaces()
	m.cfg.logger.Info().Int("spaces", len(spaces)).Msg("Starting migration")

	// loadCtx is cancelled when the producer finishes (or fails) so that the
	// Load goroutine — which blocks reading from the channels — is not left
	// waiting forever if we return early from an error.
	loadCtx, cancelLoad := context.WithCancel(svcCtx)
	defer cancelLoad()

	var wg sync.WaitGroup
	var loaderError error
	wg.Go(func() {
		loaderError = m.cfg.loader.Load(loadCtx, m.sharesChan, m.receivedChan)
	})

	migrated := 0
	for _, space := range spaces {
		sharesCreated, err := m.migrateSpace(loadCtx, space)
		if err != nil {
			m.cfg.logger.Error().Err(err).Str("space", space.GetId().GetOpaqueId()).Msg("failed to migrate space; continuing with remaining spaces")
			continue
		}
		migrated++
		m.cfg.logger.Debug().
			Str("space", space.GetId().GetOpaqueId()).
			Int("shares_created", sharesCreated).
			Msg("space migrated")
		if migrated%10 == 0 {
			m.cfg.logger.Info().
				Int("migrated", migrated).
				Int("total", len(spaces)).
				Msg("migration progress")
		}
	}
	close(m.receivedChan)
	close(m.sharesChan)

	wg.Wait()
	m.cfg.logger.Info().Err(loaderError).Int("migrated", migrated).Int("total", len(spaces)).Msg("Migration finished")
	return loaderError
}

func (m *ImportSpaceMembersMigration) migrateSpace(ctx context.Context, space *provider.StorageSpace) (int, error) {
	spClient, err := m.storageProviderForSpace(ctx, space)
	if err != nil {
		return 0, err
	}

	ref := &provider.Reference{ResourceId: space.GetRoot()}
	grantsRes, err := spClient.ListGrants(ctx, &provider.ListGrantsRequest{Ref: ref})
	if err != nil {
		return 0, err
	}
	if grantsRes.GetStatus().GetCode() != rpc.Code_CODE_OK {
		return 0, errtypes.NewErrtypeFromStatus(grantsRes.GetStatus())
	}

	sharesCreated := 0
	for _, grant := range grantsRes.GetGrants() {
		share, receivedShares, err := m.spaceGrantToShares(ctx, grant, space)
		if err != nil {
			m.cfg.logger.Error().Err(err).
				Interface("grant", grant).
				Msg("Failed to convert grant to shares")
			continue
		}
		if share == nil {
			// share already existed; nothing to import for this grant
			continue
		}

		select {
		case m.sharesChan <- share:
		case <-ctx.Done():
			return sharesCreated, ctx.Err()
		}
		for _, rs := range receivedShares {
			select {
			case m.receivedChan <- rs:
			case <-ctx.Done():
				return sharesCreated, ctx.Err()
			}
		}
		sharesCreated++
	}
	return sharesCreated, nil
}

func (m *ImportSpaceMembersMigration) spaceGrantToShares(ctx context.Context, grant *provider.Grant, space *provider.StorageSpace) (*collaboration.Share, []share.ReceivedShareWithUser, error) {
	// The grantee ids as peristed on disk do not have a IDP and type stored as part of the userid/groupid.
	// We either need to do a lookup via the user/groupprovider or set a default IDP value via config
	// FIXME get rid of hardcoded IDP value
	idp := "https://localhost:9200"
	switch grant.GetGrantee().GetType() {
	case provider.GranteeType_GRANTEE_TYPE_GROUP:
		grant.Grantee.Id = &provider.Grantee_GroupId{
			GroupId: &grouppb.GroupId{
				OpaqueId: grant.GetGrantee().GetGroupId().GetOpaqueId(),
				Type:     grouppb.GroupType_GROUP_TYPE_REGULAR,
				Idp:      idp,
			},
		}
	case provider.GranteeType_GRANTEE_TYPE_USER:
		grant.Grantee.Id = &provider.Grantee_UserId{
			UserId: &userpb.UserId{
				OpaqueId: grant.GetGrantee().GetUserId().GetOpaqueId(),
				Type:     userpb.UserType_USER_TYPE_PRIMARY,
				Idp:      idp,
			},
		}
	}

	ref := &collaboration.ShareReference{
		Spec: &collaboration.ShareReference_Key{
			Key: &collaboration.ShareKey{
				ResourceId: space.GetRoot(),
				Grantee:    grant.GetGrantee(),
			},
		},
	}

	ctx = ctxpkg.ContextSetUser(ctx, &userpb.User{Id: grant.Creator})
	if s, err := m.cfg.manager.GetShare(ctx, ref); err == nil {
		// FIXME: Verify the actual grants?
		m.cfg.logger.Debug().Interface("share", s).Msg("share already exists")
		return nil, nil, nil
	}

	ts := utils.TSNow()
	shareID := shareid.Encode(space.GetRoot().GetStorageId(), space.GetRoot().GetSpaceId(), uuid.NewString())

	creator := grant.GetCreator()
	if creator.Type == userpb.UserType_USER_TYPE_INVALID {
		creator = nil
	}
	newShare := &collaboration.Share{
		Id:          &collaboration.ShareId{OpaqueId: shareID},
		ResourceId:  space.GetRoot(),
		Permissions: &collaboration.SharePermissions{Permissions: grant.GetPermissions()},
		Grantee:     grant.GetGrantee(),
		Expiration:  grant.GetExpiration(),
		Owner:       creator,
		Creator:     creator,
		Ctime:       ts,
		Mtime:       ts,
	}

	var newReceivedShares []share.ReceivedShareWithUser
	switch grant.GetGrantee().GetType() {
	case provider.GranteeType_GRANTEE_TYPE_GROUP:
		gwc, err := m.cfg.gatewaySelector.Next()
		if err != nil {
			m.cfg.logger.Error().Err(err).Msg("Failed to get gateway client")
			return nil, nil, err
		}

		gr, err := gwc.GetMembers(ctx, &grouppb.GetMembersRequest{
			GroupId: grant.GetGrantee().GetGroupId(),
		})
		if err != nil {
			m.cfg.logger.Error().Err(err).Msg("Failed to expand group membership")
			return nil, nil, err
		}
		if gr.GetStatus().GetCode() != rpc.Code_CODE_OK {
			m.cfg.logger.Error().Str("Status", gr.GetStatus().GetMessage()).Msg("Failed to expand group membership")
			return nil, nil, errtypes.NewErrtypeFromStatus(gr.GetStatus())
		}
		for _, u := range gr.GetMembers() {
			newReceivedShares = append(newReceivedShares, share.ReceivedShareWithUser{
				UserID: u,
				ReceivedShare: &collaboration.ReceivedShare{
					Share: newShare,
					State: collaboration.ShareState_SHARE_STATE_ACCEPTED,
				},
			})
		}
		// Also add a group-level entry (UserID == nil) so the group cache is populated.
		newReceivedShares = append(newReceivedShares, share.ReceivedShareWithUser{
			UserID: nil,
			ReceivedShare: &collaboration.ReceivedShare{
				Share: newShare,
				State: collaboration.ShareState_SHARE_STATE_ACCEPTED,
			},
		})
	case provider.GranteeType_GRANTEE_TYPE_USER:
		newReceivedShares = append(newReceivedShares, share.ReceivedShareWithUser{
			UserID: grant.GetGrantee().GetUserId(),
			ReceivedShare: &collaboration.ReceivedShare{
				Share: newShare,
				State: collaboration.ShareState_SHARE_STATE_ACCEPTED,
			},
		})
	}
	return newShare, newReceivedShares, nil
}

// storageProviderForSpace resolves the storageprovider responsible for the
// given storage space and returns a dialled client. In the default opencloud
// deployment the storage registry is co-located with the gateway, so
// the GatewayAddr is used as the registry address.
func (m *ImportSpaceMembersMigration) storageProviderForSpace(ctx context.Context, space *provider.StorageSpace) (provider.ProviderAPIClient, error) {

	srClient, err := pool.GetStorageRegistryClient(m.cfg.providerRegistryAddr)
	if err != nil {
		return nil, fmt.Errorf("get storage registry client: %w", err)
	}

	spaceJSON, err := json.Marshal(space)
	if err != nil {
		return nil, fmt.Errorf("marshal space: %w", err)
	}

	res, err := srClient.GetStorageProviders(ctx, &registry.GetStorageProvidersRequest{
		Opaque: &typesv1beta1.Opaque{
			Map: map[string]*typesv1beta1.OpaqueEntry{
				"space": {
					Decoder: "json",
					Value:   spaceJSON,
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("GetStorageProviders: %w", err)
	}
	if len(res.GetProviders()) == 0 {
		return nil, fmt.Errorf("no storage provider found for space %s", space.GetId().GetOpaqueId())
	}

	c, err := pool.GetStorageProviderServiceClient(res.GetProviders()[0].GetAddress())
	if err != nil {
		return nil, fmt.Errorf("dial storage provider: %w", err)
	}
	return c, nil
}
