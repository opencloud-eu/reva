package ldap

import (
	"testing"

	"github.com/go-ldap/ldap/v3"
	"github.com/google/uuid"
)

func TestGetUserFindFilter(t *testing.T) {
	tests := []struct {
		name                string
		substringFilterType string
		userFilter          string
		objectClass         string
		mailAttr            string
		displayNameAttr     string
		usernameAttr        string
		idAttr              string
		idIsOctetString     bool
		tenantIDAttr        string
		query               string
		tenantID            string
		expectedFilter      string
	}{
		{
			name:                "initial substring filter without tenant",
			substringFilterType: "initial",
			userFilter:          "",
			objectClass:         "posixAccount",
			mailAttr:            "mail",
			displayNameAttr:     "displayName",
			usernameAttr:        "cn",
			idAttr:              "ms-DS-ConsistencyGuid",
			idIsOctetString:     false,
			tenantIDAttr:        "",
			query:               "john",
			tenantID:            "",
			expectedFilter:      "(&(objectclass=posixAccount)(|(mail=john*)(displayName=john*)(cn=john*)(ms-DS-ConsistencyGuid=john)))",
		},
		{
			name:                "any substring filter",
			substringFilterType: "any",
			userFilter:          "",
			objectClass:         "posixAccount",
			mailAttr:            "mail",
			displayNameAttr:     "displayName",
			usernameAttr:        "cn",
			idAttr:              "uid",
			idIsOctetString:     false,
			tenantIDAttr:        "",
			query:               "doe",
			tenantID:            "",
			expectedFilter:      "(&(objectclass=posixAccount)(|(mail=*doe*)(displayName=*doe*)(cn=*doe*)(uid=doe)))",
		},
		{
			name:                "final substring filter",
			substringFilterType: "final",
			userFilter:          "",
			objectClass:         "posixAccount",
			mailAttr:            "mail",
			displayNameAttr:     "displayName",
			usernameAttr:        "cn",
			idAttr:              "uid",
			idIsOctetString:     false,
			tenantIDAttr:        "",
			query:               "smith",
			tenantID:            "",
			expectedFilter:      "(&(objectclass=posixAccount)(|(mail=*smith)(displayName=*smith)(cn=*smith)(uid=smith)))",
		},
		{
			name:                "with custom user filter",
			substringFilterType: "initial",
			userFilter:          "(!(userAccountControl:1.2.840.113556.1.4.803:=2))",
			objectClass:         "user",
			mailAttr:            "mail",
			displayNameAttr:     "displayName",
			usernameAttr:        "sAMAccountName",
			idAttr:              "objectGUID",
			idIsOctetString:     false,
			tenantIDAttr:        "",
			query:               "alice",
			tenantID:            "",
			expectedFilter:      "(&(!(userAccountControl:1.2.840.113556.1.4.803:=2))(objectclass=user)(|(mail=alice*)(displayName=alice*)(sAMAccountName=alice*)(objectGUID=alice)))",
		},
		{
			name:                "with tenant filter",
			substringFilterType: "initial",
			userFilter:          "",
			objectClass:         "posixAccount",
			mailAttr:            "mail",
			displayNameAttr:     "displayName",
			usernameAttr:        "cn",
			idAttr:              "uid",
			idIsOctetString:     false,
			tenantIDAttr:        "tenantId",
			query:               "bob",
			tenantID:            "tenant1",
			expectedFilter:      "(&(objectclass=posixAccount)(tenantId=tenant1)(|(mail=bob*)(displayName=bob*)(cn=bob*)(uid=bob)))",
		},
		{
			name:                "special characters are escaped",
			substringFilterType: "initial",
			userFilter:          "",
			objectClass:         "posixAccount",
			mailAttr:            "mail",
			displayNameAttr:     "displayName",
			usernameAttr:        "cn",
			idAttr:              "uid",
			idIsOctetString:     false,
			tenantIDAttr:        "",
			query:               "test*(user)",
			tenantID:            "",
			expectedFilter:      "(&(objectclass=posixAccount)(|(mail=test\\2a\\28user\\29*)(displayName=test\\2a\\28user\\29*)(cn=test\\2a\\28user\\29*)(uid=test\\2a\\28user\\29)))",
		},
		{
			name:                "octet string ID with valid UUID",
			substringFilterType: "initial",
			userFilter:          "",
			objectClass:         "user",
			mailAttr:            "mail",
			displayNameAttr:     "displayName",
			usernameAttr:        "sAMAccountName",
			idAttr:              "objectGUID",
			idIsOctetString:     true,
			tenantIDAttr:        "",
			query:               "550e8400-e29b-41d4-a716-446655440000",
			tenantID:            "",
			expectedFilter:      "(&(objectclass=user)(|(mail=550e8400-e29b-41d4-a716-446655440000*)(displayName=550e8400-e29b-41d4-a716-446655440000*)(sAMAccountName=550e8400-e29b-41d4-a716-446655440000*)(objectGUID=\\00\\84\\0e\\55\\9b\\e2\\d4\\41\\a7\\16\\44\\66\\55\\44\\00\\00)))",
		},
		{
			name:                "empty query",
			substringFilterType: "initial",
			userFilter:          "",
			objectClass:         "posixAccount",
			mailAttr:            "mail",
			displayNameAttr:     "displayName",
			usernameAttr:        "cn",
			idAttr:              "uid",
			idIsOctetString:     false,
			tenantIDAttr:        "",
			query:               "",
			tenantID:            "",
			expectedFilter:      "(&(objectclass=posixAccount)(|(mail=*)(displayName=*)(cn=*)(uid=)))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := &Identity{
				User: userConfig{
					Filter:              tt.userFilter,
					Objectclass:         tt.objectClass,
					SubstringFilterType: tt.substringFilterType,
					Schema: userSchema{
						Mail:            tt.mailAttr,
						DisplayName:     tt.displayNameAttr,
						Username:        tt.usernameAttr,
						ID:              tt.idAttr,
						IDIsOctetString: tt.idIsOctetString,
						TenantID:        tt.tenantIDAttr,
					},
				},
				Group: groupConfig{
					Schema: groupSchema{
						IDIsOctetString: tt.idIsOctetString,
					},
				},
			}

			// Setup the substring filter value
			switch tt.substringFilterType {
			case "initial":
				i.User.substringFilterVal = ldap.FilterSubstringsInitial
			case "any":
				i.User.substringFilterVal = ldap.FilterSubstringsAny
			case "final":
				i.User.substringFilterVal = ldap.FilterSubstringsFinal
			}

			result := i.getUserFindFilter(tt.query, tt.tenantID)

			if result != tt.expectedFilter {
				t.Errorf("getUserFindFilter() = %v, want %v", result, tt.expectedFilter)
			}
		})
	}
}

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
