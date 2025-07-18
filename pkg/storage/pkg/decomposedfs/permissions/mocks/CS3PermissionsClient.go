// Copyright 2018-2022 CERN
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

// Code generated by mockery v2.53.2. DO NOT EDIT.

package mocks

import (
	context "context"

	grpc "google.golang.org/grpc"

	mock "github.com/stretchr/testify/mock"

	permissionsv1beta1 "github.com/cs3org/go-cs3apis/cs3/permissions/v1beta1"
)

// CS3PermissionsClient is an autogenerated mock type for the CS3PermissionsClient type
type CS3PermissionsClient struct {
	mock.Mock
}

type CS3PermissionsClient_Expecter struct {
	mock *mock.Mock
}

func (_m *CS3PermissionsClient) EXPECT() *CS3PermissionsClient_Expecter {
	return &CS3PermissionsClient_Expecter{mock: &_m.Mock}
}

// CheckPermission provides a mock function with given fields: ctx, in, opts
func (_m *CS3PermissionsClient) CheckPermission(ctx context.Context, in *permissionsv1beta1.CheckPermissionRequest, opts ...grpc.CallOption) (*permissionsv1beta1.CheckPermissionResponse, error) {
	var tmpRet mock.Arguments
	if len(opts) > 0 {
		tmpRet = _m.Called(ctx, in, opts)
	} else {
		tmpRet = _m.Called(ctx, in)
	}
	ret := tmpRet

	if len(ret) == 0 {
		panic("no return value specified for CheckPermission")
	}

	var r0 *permissionsv1beta1.CheckPermissionResponse
	var r1 error
	if rf, ok := ret.Get(0).(func(context.Context, *permissionsv1beta1.CheckPermissionRequest, ...grpc.CallOption) (*permissionsv1beta1.CheckPermissionResponse, error)); ok {
		return rf(ctx, in, opts...)
	}
	if rf, ok := ret.Get(0).(func(context.Context, *permissionsv1beta1.CheckPermissionRequest, ...grpc.CallOption) *permissionsv1beta1.CheckPermissionResponse); ok {
		r0 = rf(ctx, in, opts...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*permissionsv1beta1.CheckPermissionResponse)
		}
	}

	if rf, ok := ret.Get(1).(func(context.Context, *permissionsv1beta1.CheckPermissionRequest, ...grpc.CallOption) error); ok {
		r1 = rf(ctx, in, opts...)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// CS3PermissionsClient_CheckPermission_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'CheckPermission'
type CS3PermissionsClient_CheckPermission_Call struct {
	*mock.Call
}

// CheckPermission is a helper method to define mock.On call
//   - ctx context.Context
//   - in *permissionsv1beta1.CheckPermissionRequest
//   - opts ...grpc.CallOption
func (_e *CS3PermissionsClient_Expecter) CheckPermission(ctx interface{}, in interface{}, opts ...interface{}) *CS3PermissionsClient_CheckPermission_Call {
	return &CS3PermissionsClient_CheckPermission_Call{Call: _e.mock.On("CheckPermission",
		append([]interface{}{ctx, in}, opts...)...)}
}

func (_c *CS3PermissionsClient_CheckPermission_Call) Run(run func(ctx context.Context, in *permissionsv1beta1.CheckPermissionRequest, opts ...grpc.CallOption)) *CS3PermissionsClient_CheckPermission_Call {
	_c.Call.Run(func(args mock.Arguments) {
		variadicArgs := make([]grpc.CallOption, len(args)-2)
		for i, a := range args[2:] {
			if a != nil {
				variadicArgs[i] = a.(grpc.CallOption)
			}
		}
		run(args[0].(context.Context), args[1].(*permissionsv1beta1.CheckPermissionRequest), variadicArgs...)
	})
	return _c
}

func (_c *CS3PermissionsClient_CheckPermission_Call) Return(_a0 *permissionsv1beta1.CheckPermissionResponse, _a1 error) *CS3PermissionsClient_CheckPermission_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

func (_c *CS3PermissionsClient_CheckPermission_Call) RunAndReturn(run func(context.Context, *permissionsv1beta1.CheckPermissionRequest, ...grpc.CallOption) (*permissionsv1beta1.CheckPermissionResponse, error)) *CS3PermissionsClient_CheckPermission_Call {
	_c.Call.Return(run)
	return _c
}

// NewCS3PermissionsClient creates a new instance of CS3PermissionsClient. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewCS3PermissionsClient(t interface {
	mock.TestingT
	Cleanup(func())
}) *CS3PermissionsClient {
	mock := &CS3PermissionsClient{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
