package json_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	ocmprovider "github.com/cs3org/go-cs3apis/cs3/ocm/provider/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opencloud-eu/reva/v3/pkg/errtypes"
	"github.com/opencloud-eu/reva/v3/pkg/ocm/provider/authorizer/json"
)

func TestAuthorizer_GetInfoByDomain(t *testing.T) {
	authorizer, err := json.New(map[string]interface{}{
		"providers": "./testdata/providers.json",
	})
	assert.NotNil(t, authorizer)
	assert.Nil(t, err)

	{ // implicit normalizeDomain
		for name, env := range map[string]struct {
			givenDomain    string
			expectedDomain string
			expectedError  error
		}{
			"domain only":                     {givenDomain: "server-one", expectedDomain: "server-one"},
			"domain with port":                {givenDomain: "server-two:9200", expectedDomain: "server-two:9200"},
			"domain only with port in result": {givenDomain: "server-two", expectedDomain: "server-two:9200"},
			"unknown domain":                  {givenDomain: "unknown-domain", expectedError: errtypes.NotFound("unknown-domain")},
		} {
			t.Run(name, func(t *testing.T) {
				info, err := authorizer.GetInfoByDomain(context.Background(), env.givenDomain)
				assert.ErrorIs(t, err, env.expectedError)
				assert.Equal(t, info.GetDomain(), env.expectedDomain)
			})
		}
	}
}

func TestAuthorizer_IsProviderAllowed(t *testing.T) {
	{ // implicit normalizeDomain
		for name, env := range map[string]struct {
			providerInfo          *ocmprovider.ProviderInfo
			verifyRequestHostname bool
			expectedError         error
		}{
			"not authorized": {
				providerInfo: &ocmprovider.ProviderInfo{
					Domain: "some.unknown.domain",
				},
				expectedError: errtypes.NotFound("some.unknown.domain"),
			},
			"authorized without host name verification": {
				providerInfo: &ocmprovider.ProviderInfo{
					Domain: "server-one",
				},
			},
			"no services and host name verification enabled": {
				providerInfo:          &ocmprovider.ProviderInfo{},
				verifyRequestHostname: true,
				expectedError:         json.ErrNoIP,
			},
			"fails if the domain contains a port": {
				providerInfo: &ocmprovider.ProviderInfo{
					Domain: "server-two",
				},
				expectedError: error(errtypes.NotFound("server-two")),
			},
		} {
			t.Run(name, func(t *testing.T) {
				authorizer, err := json.New(map[string]interface{}{
					"providers":               "./testdata/providers.json",
					"verify_request_hostname": env.verifyRequestHostname,
				})
				assert.NotNil(t, authorizer)
				assert.Nil(t, err)

				err = authorizer.IsProviderAllowed(context.Background(), env.providerInfo)
				assert.ErrorIs(t, err, env.expectedError)
			})
		}
	}
}

func TestAuthorizer_GetInfoByDomainWildcard(t *testing.T) {
	providers := filepath.Join(t.TempDir(), "providers.json")
	err := os.WriteFile(providers, []byte(`[{
		"domain": ".*\\.example\\.org",
		"services": [{
			"endpoint": {"type": {"name": "OCM"}, "path": "https://.*\\.example\\.org/ocm/"},
			"host": "https://.*\\.example\\.org"
		}]
	}]`), 0600)
	assert.Nil(t, err)

	authorizer, err := json.New(map[string]interface{}{
		"providers": providers,
	})
	assert.Nil(t, err)

	// every lookup must resolve the wildcard on its own, the first must not
	// overwrite the configured provider for the following ones
	for _, domain := range []string{"one.example.org", "two.example.org", "one.example.org"} {
		info, err := authorizer.GetInfoByDomain(context.Background(), domain)
		require.NoError(t, err)
		assert.Equal(t, domain, info.GetDomain())
		assert.Equal(t, "https://"+domain, info.GetServices()[0].GetHost())
		assert.Equal(t, "https://"+domain+"/ocm/", info.GetServices()[0].GetEndpoint().GetPath())
	}
}
