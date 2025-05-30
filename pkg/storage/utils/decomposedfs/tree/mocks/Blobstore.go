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
	io "io"

	node "github.com/opencloud-eu/reva/v2/pkg/storage/utils/decomposedfs/node"
	mock "github.com/stretchr/testify/mock"
)

// Blobstore is an autogenerated mock type for the Blobstore type
type Blobstore struct {
	mock.Mock
}

type Blobstore_Expecter struct {
	mock *mock.Mock
}

func (_m *Blobstore) EXPECT() *Blobstore_Expecter {
	return &Blobstore_Expecter{mock: &_m.Mock}
}

// Delete provides a mock function with given fields: _a0
func (_m *Blobstore) Delete(_a0 *node.Node) error {
	ret := _m.Called(_a0)

	if len(ret) == 0 {
		panic("no return value specified for Delete")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(*node.Node) error); ok {
		r0 = rf(_a0)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// Blobstore_Delete_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Delete'
type Blobstore_Delete_Call struct {
	*mock.Call
}

// Delete is a helper method to define mock.On call
//   - _a0 *node.Node
func (_e *Blobstore_Expecter) Delete(_a0 interface{}) *Blobstore_Delete_Call {
	return &Blobstore_Delete_Call{Call: _e.mock.On("Delete", _a0)}
}

func (_c *Blobstore_Delete_Call) Run(run func(_a0 *node.Node)) *Blobstore_Delete_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(*node.Node))
	})
	return _c
}

func (_c *Blobstore_Delete_Call) Return(_a0 error) *Blobstore_Delete_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *Blobstore_Delete_Call) RunAndReturn(run func(*node.Node) error) *Blobstore_Delete_Call {
	_c.Call.Return(run)
	return _c
}

// Download provides a mock function with given fields: _a0
func (_m *Blobstore) Download(_a0 *node.Node) (io.ReadCloser, error) {
	ret := _m.Called(_a0)

	if len(ret) == 0 {
		panic("no return value specified for Download")
	}

	var r0 io.ReadCloser
	var r1 error
	if rf, ok := ret.Get(0).(func(*node.Node) (io.ReadCloser, error)); ok {
		return rf(_a0)
	}
	if rf, ok := ret.Get(0).(func(*node.Node) io.ReadCloser); ok {
		r0 = rf(_a0)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(io.ReadCloser)
		}
	}

	if rf, ok := ret.Get(1).(func(*node.Node) error); ok {
		r1 = rf(_a0)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// Blobstore_Download_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Download'
type Blobstore_Download_Call struct {
	*mock.Call
}

// Download is a helper method to define mock.On call
//   - _a0 *node.Node
func (_e *Blobstore_Expecter) Download(_a0 interface{}) *Blobstore_Download_Call {
	return &Blobstore_Download_Call{Call: _e.mock.On("Download", _a0)}
}

func (_c *Blobstore_Download_Call) Run(run func(_a0 *node.Node)) *Blobstore_Download_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(*node.Node))
	})
	return _c
}

func (_c *Blobstore_Download_Call) Return(_a0 io.ReadCloser, _a1 error) *Blobstore_Download_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

func (_c *Blobstore_Download_Call) RunAndReturn(run func(*node.Node) (io.ReadCloser, error)) *Blobstore_Download_Call {
	_c.Call.Return(run)
	return _c
}

// Upload provides a mock function with given fields: _a0, source
func (_m *Blobstore) Upload(_a0 *node.Node, source string) error {
	ret := _m.Called(_a0, source)

	if len(ret) == 0 {
		panic("no return value specified for Upload")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(*node.Node, string) error); ok {
		r0 = rf(_a0, source)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// Blobstore_Upload_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Upload'
type Blobstore_Upload_Call struct {
	*mock.Call
}

// Upload is a helper method to define mock.On call
//   - _a0 *node.Node
//   - source string
func (_e *Blobstore_Expecter) Upload(_a0 interface{}, source interface{}) *Blobstore_Upload_Call {
	return &Blobstore_Upload_Call{Call: _e.mock.On("Upload", _a0, source)}
}

func (_c *Blobstore_Upload_Call) Run(run func(_a0 *node.Node, source string)) *Blobstore_Upload_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(*node.Node), args[1].(string))
	})
	return _c
}

func (_c *Blobstore_Upload_Call) Return(_a0 error) *Blobstore_Upload_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *Blobstore_Upload_Call) RunAndReturn(run func(*node.Node, string) error) *Blobstore_Upload_Call {
	_c.Call.Return(run)
	return _c
}

// NewBlobstore creates a new instance of Blobstore. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewBlobstore(t interface {
	mock.TestingT
	Cleanup(func())
}) *Blobstore {
	mock := &Blobstore{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
