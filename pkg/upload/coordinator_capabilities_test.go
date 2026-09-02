package upload

import (
	"context"
	"testing"

	"github.com/opencloud-eu/reva/v2/pkg/storage"
)

type capabilityTestFS struct {
	storage.FS
	capabilities storage.Capabilities
}

func (fs capabilityTestFS) Capabilities(context.Context) storage.Capabilities {
	return fs.capabilities
}

func TestCoordinatorCapabilitiesDefaultsToFullSet(t *testing.T) {
	c := NewCoordinator(nil, nil, "", nil)
	if got, want := c.Capabilities(context.Background()), storage.FullCapabilities(); got != want {
		t.Errorf("Capabilities() = %#v, want %#v", got, want)
	}
}

func TestCoordinatorCapabilitiesUsesDriverDeclaration(t *testing.T) {
	want := storage.Capabilities{Upload: true, Trash: true}
	c := NewCoordinator(capabilityTestFS{capabilities: want}, nil, "", nil)
	if got := c.Capabilities(context.Background()); got != want {
		t.Errorf("Capabilities() = %#v, want %#v", got, want)
	}
}
