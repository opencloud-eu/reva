package ldap

import (
	"testing"

	"github.com/google/uuid"
)

func TestFilterEscapeBinaryUUID(t *testing.T) {
	tests := []struct {
		name      string
		attribute string
		uuidStr   string
		want      string
	}{
		{
			name:      "objectGUID with AD mixed endianness",
			attribute: "objectGUID",
			uuidStr:   "d227cf8e-986e-4a05-8860-af193056278a",
			want:      "\\8e\\cf\\27\\d2\\6e\\98\\05\\4a\\88\\60\\af\\19\\30\\56\\27\\8a",
		},
		{
			name:      "objectGUID case insensitive",
			attribute: "ObjectGuid",
			uuidStr:   "d227cf8e-986e-4a05-8860-af193056278a",
			want:      "\\8e\\cf\\27\\d2\\6e\\98\\05\\4a\\88\\60\\af\\19\\30\\56\\27\\8a",
		},
		{
			name:      "objectGUID uppercase",
			attribute: "OBJECTGUID",
			uuidStr:   "d227cf8e-986e-4a05-8860-af193056278a",
			want:      "\\8e\\cf\\27\\d2\\6e\\98\\05\\4a\\88\\60\\af\\19\\30\\56\\27\\8a",
		},
		{
			name:      "other attribute no byte swapping",
			attribute: "entryUUID",
			uuidStr:   "550e8400-e29b-41d4-a716-446655440000",
			want:      "\\55\\0e\\84\\00\\e2\\9b\\41\\d4\\a7\\16\\44\\66\\55\\44\\00\\00",
		},
		{
			name:      "other attribute with different UUID",
			attribute: "someOtherId",
			uuidStr:   "123e4567-e89b-12d3-a456-426614174000",
			want:      "\\12\\3e\\45\\67\\e8\\9b\\12\\d3\\a4\\56\\42\\66\\14\\17\\40\\00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uuidVal, err := uuid.Parse(tt.uuidStr)
			if err != nil {
				t.Fatalf("failed to parse UUID: %v", err)
			}

			got := filterEscapeBinaryUUID(tt.attribute, uuidVal)
			if got != tt.want {
				t.Errorf("filterEscapeBinaryUUID() = %v, want %v", got, tt.want)
			}
		})
	}
}
