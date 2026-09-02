package ocs

import (
	"encoding/json"
	"encoding/xml"
	"testing"

	"github.com/opencloud-eu/reva/v2/pkg/storage"
)

func TestNewProviderCapabilitiesReadOnly(t *testing.T) {
	pc := NewProviderCapabilities(storage.Capabilities{})

	b, err := json.Marshal(pc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]bool
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for k, v := range got {
		if v {
			t.Errorf("absent capability %q marshalled to true, want false", k)
		}
	}
	if _, ok := got["trash"]; !ok {
		t.Error("expected a trash key in the per-provider section")
	}
}

func TestNewProviderCapabilitiesFullSet(t *testing.T) {
	pc := NewProviderCapabilities(storage.FullCapabilities())

	b, err := json.Marshal(pc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]bool
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for k, v := range got {
		if !v {
			t.Errorf("capability %q marshalled to false, want true", k)
		}
	}
}

// TestProviderCapabilitiesXMLParity guards the ocsBool 1/0 XML rendering the OCS
// API requires, so the per-provider section stays consistent across formats.
func TestProviderCapabilitiesXMLParity(t *testing.T) {
	pc := NewProviderCapabilities(storage.Capabilities{Upload: true})

	b, err := xml.Marshal(pc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	out := string(b)
	if want := "<upload>1</upload>"; !contains(out, want) {
		t.Errorf("xml %q missing %q", out, want)
	}
	if want := "<trash>0</trash>"; !contains(out, want) {
		t.Errorf("xml %q missing %q", out, want)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
