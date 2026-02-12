// Copyright 2018-2021 CERN
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

package grpc_test

import (
	"context"
	"sync"

	appauthpb "github.com/cs3org/go-cs3apis/cs3/auth/applications/v1beta1"
	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	rpcv1beta1 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/auth/scope"
	ctxpkg "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
	jwt "github.com/opencloud-eu/reva/v2/pkg/token/manager/jwt"
	"google.golang.org/grpc/metadata"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("applicationauth providers", func() {
	var (
		dependencies []RevadConfig
		revads       map[string]*Revad
		variables    map[string]string

		serviceClient appauthpb.ApplicationsAPIClient

		ctx context.Context
	)

	JustBeforeEach(func() {
		var err error
		ctx = context.Background()

		// Add auth token
		user := &userpb.User{
			Id: &userpb.UserId{
				Idp:      "http://idp",
				OpaqueId: "f7fbf8c8-139b-4376-b307-cf0a8c2d0d9c",
				Type:     userpb.UserType_USER_TYPE_PRIMARY,
			},
		}
		tokenManager, err := jwt.New(map[string]interface{}{"secret": "secret"})
		Expect(err).ToNot(HaveOccurred())
		scope, err := scope.AddOwnerScope(nil)
		Expect(err).ToNot(HaveOccurred())
		t, err := tokenManager.MintToken(ctx, user, scope)
		Expect(err).ToNot(HaveOccurred())
		ctx = ctxpkg.ContextSetToken(ctx, t)
		ctx = metadata.AppendToOutgoingContext(ctx, ctxpkg.TokenHeader, t)
		ctx = ctxpkg.ContextSetUser(ctx, user)

		revads, err = startRevads(dependencies, variables)
		Expect(err).ToNot(HaveOccurred())
		serviceClient, err = pool.GetAppAuthProviderServiceClient(revads["applicationauth"].GrpcAddress)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		for _, r := range revads {
			Expect(r.Cleanup(CurrentSpecReport().Failed())).To(Succeed())
		}
	})

	Describe("the jsoncs3 appauth manager", func() {
		BeforeEach(func() {
			variables = map[string]string{
				"storage_system_grpc_address": "localhost:19002",
				"storage_system_http_address": "localhost:19003",
			}
			dependencies = []RevadConfig{
				{
					Name:   "applicationauth",
					Config: "applicationauth-jsoncs3.toml",
				},
				{
					Name:   "storage-system",
					Config: "storage-system.toml",
				},
			}
		})
		It("creates an app password", func() {
			servResp, err := serviceClient.GenerateAppPassword(ctx, &appauthpb.GenerateAppPasswordRequest{})
			Expect(err).ToNot(HaveOccurred())
			Expect(servResp).ToNot(BeNil())
			Expect(servResp.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))
		})
		It("verifies an app password", func() {
			servResp, err := serviceClient.GenerateAppPassword(ctx, &appauthpb.GenerateAppPasswordRequest{})
			Expect(err).ToNot(HaveOccurred())
			Expect(servResp).ToNot(BeNil())
			Expect(servResp.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))

			resp, err := serviceClient.GetAppPassword(ctx, &appauthpb.GetAppPasswordRequest{
				User:     ctxpkg.ContextMustGetUser(ctx).Id,
				Password: servResp.GetAppPassword().GetPassword(),
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			Expect(resp.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))

			// make sure fail with a wrong password
			resp, err = serviceClient.GetAppPassword(ctx, &appauthpb.GetAppPasswordRequest{
				User:     ctxpkg.ContextMustGetUser(ctx).Id,
				Password: "",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			Expect(resp.Status.Code).To(Equal(rpcv1beta1.Code_CODE_NOT_FOUND))
		})
		It("handles conncurrent access", func() {
			servResp, err := serviceClient.GenerateAppPassword(ctx, &appauthpb.GenerateAppPasswordRequest{})
			Expect(err).ToNot(HaveOccurred())
			Expect(servResp).ToNot(BeNil())
			Expect(servResp.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))

			var numIterations = 10
			var concurrency = 3
			wg := &sync.WaitGroup{}
			wg.Add(concurrency)
			for i := 0; i < concurrency; i++ {
				go func(wg *sync.WaitGroup) {
					defer GinkgoRecover()
					defer wg.Done()
					for j := 0; j < numIterations; j++ {
						resp, err := serviceClient.GetAppPassword(ctx, &appauthpb.GetAppPasswordRequest{
							User:     ctxpkg.ContextMustGetUser(ctx).Id,
							Password: servResp.GetAppPassword().GetPassword(),
						})
						Expect(err).ToNot(HaveOccurred())
						Expect(resp).ToNot(BeNil())
						Expect(resp.Status.Code).To(Equal(rpcv1beta1.Code_CODE_OK))
					}
				}(wg)
			}
			wg.Wait()
			Expect(true).ToNot(BeFalse())
		})
	})
})
