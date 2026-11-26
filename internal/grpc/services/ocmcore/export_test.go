// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package ocmcore

import (
	"github.com/opencloud-eu/reva/v3/pkg/events"
	"github.com/opencloud-eu/reva/v3/pkg/rgrpc"
)

// SetEventStream replaces the event stream of the service, tests use it to
// capture the published events without a nats server.
func SetEventStream(s rgrpc.Service, es events.Stream) {
	s.(*service).eventStream = es
}
