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

package memory

import (
	"context"

	tenantpb "github.com/cs3org/go-cs3apis/cs3/identity/tenant/v1beta1"
	"github.com/mitchellh/mapstructure"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/tenant"
	"github.com/opencloud-eu/reva/v2/pkg/tenant/manager/registry"
	"github.com/pkg/errors"
)

func init() {
	registry.Register("memory", New)
}

type config struct {
	// Users holds a map with userid and user
	Tenants map[string]*Tenant `mapstructure:"tenants"`
}

// User holds a user but uses in mapstructure names
type Tenant struct {
	ID         string `mapstructure:"id" json:"id"`
	ExternalID string `mapstructure:"external_id" json:"external_id"`
	Name       string `mapstructure:"name" json:"name"`
}

func parseConfig(m map[string]interface{}) (*config, error) {
	c := &config{}
	if err := mapstructure.Decode(m, c); err != nil {
		err = errors.Wrap(err, "error decoding conf")
		return nil, err
	}
	if len(c.Tenants) == 0 {
		c.Tenants = map[string]*Tenant{
			"f65b26f8-2ce2-11f1-b0af-7f3198d25769": &Tenant{
				ID:         "f65b26f8-2ce2-11f1-b0af-7f3198d25769",
				ExternalID: "externalid_1",
				Name:       "Tenant One",
			},
			"f76a25ee-2ce2-11f1-9852-4fa5a531eb10": {
				ID:         "f76a25ee-2ce2-11f1-9852-4fa5a531eb10",
				ExternalID: "externalid_2",
				Name:       "Tenant Two",
			},
		}
	}
	return c, nil
}

type manager struct {
	catalog map[string]*Tenant
}

// New returns a new user manager.
func New(m map[string]interface{}) (tenant.Manager, error) {
	mgr := &manager{}
	err := mgr.Configure(m)
	return mgr, err
}

func (m *manager) Configure(ml map[string]interface{}) error {
	c, err := parseConfig(ml)
	if err != nil {
		return err
	}
	m.catalog = c.Tenants
	return nil
}

func (m *manager) GetTenant(ctx context.Context, id string) (*tenantpb.Tenant, error) {
	if t, ok := m.catalog[id]; ok {
		return &tenantpb.Tenant{
			Id:         t.ID,
			ExternalId: t.ExternalID,
			Name:       t.Name,
		}, nil
	}
	return nil, errtypes.NotFound(id)
}

func (m *manager) GetTenantByClaim(ctx context.Context, claim, value string) (*tenantpb.Tenant, error) {
	for _, t := range m.catalog {
		if tenantClaim, err := extractClaim(t, claim); err == nil && value == tenantClaim {
			return &tenantpb.Tenant{
				Id:         t.ID,
				ExternalId: t.ExternalID,
				Name:       t.Name,
			}, nil
		}
	}
	return nil, errtypes.NotFound(value)
}

func extractClaim(t *Tenant, claim string) (string, error) {
	switch claim {
	case "id":
		return t.ID, nil
	case "externalid":
		return t.ExternalID, nil
	}
	return "", errors.New("memory: invalid field")
}
