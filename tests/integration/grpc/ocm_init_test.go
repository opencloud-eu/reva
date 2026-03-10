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

package grpc_test

import (
	"os"

	"github.com/pkg/errors"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	invitepb "github.com/cs3org/go-cs3apis/cs3/ocm/invite/v1beta1"
	"github.com/opencloud-eu/reva/v2/tests/helpers"

	. "github.com/onsi/gomega"
)

func initData(driver string, tokens []*invitepb.InviteToken, acceptedUsers map[string][]*userpb.User) (map[string]string, func(), error) {
	variables := map[string]string{
		"ocm_driver": driver,
	}
	switch driver {
	case "json":
		return initJSONData(variables, tokens, acceptedUsers)
	}

	return nil, nil, errors.New("driver not found")
}

func initJSONData(variables map[string]string, tokens []*invitepb.InviteToken, acceptedUsers map[string][]*userpb.User) (map[string]string, func(), error) {
	data := map[string]any{}

	if len(tokens) != 0 {
		m := map[string]*invitepb.InviteToken{}
		for _, tkn := range tokens {
			m[tkn.Token] = tkn
		}
		data["invites"] = m
	}

	if len(acceptedUsers) != 0 {
		data["accepted_users"] = acceptedUsers
	}

	inviteTokenFile, err := helpers.TempJSONFile(data)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		Expect(os.RemoveAll(inviteTokenFile)).To(Succeed())
	}
	variables["invite_token_file"] = inviteTokenFile
	return variables, cleanup, nil
}
