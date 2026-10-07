package prefixes

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func resetOcPrefix(t *testing.T) {
	t.Cleanup(func() {
		fixed = false
		setKeys(defaultOcPrefix)
	})
}

func TestSetOcPrefixDerivesAllKeys(t *testing.T) {
	resetOcPrefix(t)

	require.NoError(t, SetOcPrefix("user.foreign."))
	require.Equal(t, "user.foreign.", OcPrefix)
	require.Equal(t, "user.foreign.id", IDAttr)
	require.Equal(t, "user.foreign.blobid", BlobIDAttr)
	require.Equal(t, "user.foreign.grant.", GrantPrefix)
	require.Equal(t, "user.foreign.space.contenttype", SpaceContentTypeAttr)
}

func TestSetOcPrefixEmptyKeepsDefault(t *testing.T) {
	resetOcPrefix(t)

	require.NoError(t, SetOcPrefix(""))
	require.Equal(t, defaultOcPrefix+"id", IDAttr)
	require.NoError(t, SetOcPrefix(defaultOcPrefix))
}

func TestSetOcPrefixIsFixedByTheFirstCall(t *testing.T) {
	resetOcPrefix(t)

	require.NoError(t, SetOcPrefix("user.foreign."))
	require.NoError(t, SetOcPrefix("user.foreign."))
	require.Error(t, SetOcPrefix("user.other."))
	require.Error(t, SetOcPrefix(""))
	require.Equal(t, "user.foreign.id", IDAttr)
}
