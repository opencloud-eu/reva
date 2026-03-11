// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package events

import (
	"encoding/json"

	user "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	types "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
)

// FavoriteAdded is emitted when a user added a resource to their favorites
type FavoriteAdded struct {
	Ref       *provider.Reference
	Executant *user.UserId
	UserID    *user.UserId
	Timestamp *types.Timestamp
}

// Unmarshal to fulfill umarshaller interface
func (FavoriteAdded) Unmarshal(v []byte) (interface{}, error) {
	e := FavoriteAdded{}
	err := json.Unmarshal(v, &e)
	return e, err
}

// FavoriteRemoved is emitted when a user removed a resource from their favorites
type FavoriteRemoved struct {
	Ref       *provider.Reference
	Executant *user.UserId
	UserID    *user.UserId
	Timestamp *types.Timestamp
}

// Unmarshal to fulfill umarshaller interface
func (FavoriteRemoved) Unmarshal(v []byte) (interface{}, error) {
	e := FavoriteRemoved{}
	err := json.Unmarshal(v, &e)
	return e, err
}
