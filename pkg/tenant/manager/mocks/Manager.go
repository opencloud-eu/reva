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

package mocks

import (
	"context"

	tenantpb "github.com/cs3org/go-cs3apis/cs3/identity/tenant/v1beta1"
	"github.com/stretchr/testify/mock"
)

// Manager is a mock implementation of the tenant.Manager interface
type Manager struct {
	mock.Mock
}

// GetTenant returns the tenant metadata identified by an id.
func (m *Manager) GetTenant(ctx context.Context, id string) (*tenantpb.Tenant, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(*tenantpb.Tenant), args.Error(1)
}

// GetTenantByClaim returns the tenant identified by a specific value for a given claim.
func (m *Manager) GetTenantByClaim(ctx context.Context, claim, value string) (*tenantpb.Tenant, error) {
	args := m.Called(ctx, claim, value)
	return args.Get(0).(*tenantpb.Tenant), args.Error(1)
}
