// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package revisioncleanup

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/options"
	"github.com/opencloud-eu/reva/v3/pkg/storagespace"
)

// The strategy of a space comes from the most specific setting that is set: the forced one,
// then the one of the space's type, then the default.
func TestPolicyCleanerForSpaceType(t *testing.T) {
	for _, tc := range []struct {
		name       string
		config     PolicyConfig
		spaceType  string
		wantConfig string // the strategy of the Cleaner, "disabled" for a disabled one
		wantSource StrategySource
	}{
		{
			name:       "only a default",
			config:     PolicyConfig{Default: "30d"},
			spaceType:  storagespace.TypePersonal,
			wantConfig: "30d",
			wantSource: SourceDefault,
		},
		{
			name:       "the type's own strategy",
			config:     PolicyConfig{Default: "30d", SpaceTypes: map[string]string{storagespace.TypePersonal: "7d, 5"}},
			spaceType:  storagespace.TypePersonal,
			wantConfig: "7d, 5",
			wantSource: SourceSpaceType,
		},
		{
			name:       "another type gets the default",
			config:     PolicyConfig{Default: "30d", SpaceTypes: map[string]string{storagespace.TypePersonal: "7d, 5"}},
			spaceType:  storagespace.TypeProject,
			wantConfig: "30d",
			wantSource: SourceDefault,
		},
		{
			name:       "an unknown type gets the default",
			config:     PolicyConfig{Default: "30d", SpaceTypes: map[string]string{storagespace.TypePersonal: "7d, 5"}},
			spaceType:  "something else",
			wantConfig: "30d",
			wantSource: SourceDefault,
		},
		{
			name:       "an empty strategy of the type leaves it to the default",
			config:     PolicyConfig{Default: "30d", SpaceTypes: map[string]string{storagespace.TypePersonal: " "}},
			spaceType:  storagespace.TypePersonal,
			wantConfig: "30d",
			wantSource: SourceDefault,
		},
		{
			name:       "a disabled type keeps all revisions despite the default",
			config:     PolicyConfig{Default: "30d", SpaceTypes: map[string]string{storagespace.TypeProject: "disabled"}},
			spaceType:  storagespace.TypeProject,
			wantConfig: "disabled",
			wantSource: SourceSpaceType,
		},
		{
			name:       "a type's strategy without a default",
			config:     PolicyConfig{SpaceTypes: map[string]string{storagespace.TypeProject: "90d"}},
			spaceType:  storagespace.TypeProject,
			wantConfig: "90d",
			wantSource: SourceSpaceType,
		},
		{
			name:       "no default for the other types",
			config:     PolicyConfig{SpaceTypes: map[string]string{storagespace.TypeProject: "90d"}},
			spaceType:  storagespace.TypePersonal,
			wantConfig: "disabled",
			wantSource: SourceDefault,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewPolicy(tc.config)
			require.NoError(t, err)
			c, source := p.cleanerForSpaceType(tc.spaceType)
			require.Equal(t, tc.wantConfig, c.config)
			require.Equal(t, tc.wantSource, source)
		})
	}
}

// A forced strategy applies to every file, without looking at its space at all, and so does
// the default when there are no strategies per type.
func TestPolicyCleanerForWithoutReadingTheSpace(t *testing.T) {
	ctx := context.Background()

	forced, err := NewPolicy(PolicyConfig{
		Default:    "30d",
		SpaceTypes: map[string]string{storagespace.TypePersonal: "7d, 5", storagespace.TypeProject: "disabled"},
		Forced:     "0",
	})
	require.NoError(t, err)
	// a file without a space root: reading the type of the space would fail
	file := testNode("a")
	c, source, err := forced.CleanerFor(ctx, file)
	require.NoError(t, err)
	require.Equal(t, "0", c.config)
	require.Equal(t, SourceForced, source)

	onlyDefault, err := NewPolicy(PolicyConfig{Default: "30d", SpaceTypes: map[string]string{storagespace.TypePersonal: ""}})
	require.NoError(t, err)
	c, source, err = onlyDefault.CleanerFor(ctx, file)
	require.NoError(t, err)
	require.Equal(t, "30d", c.config)
	require.Equal(t, SourceDefault, source)

	// with strategies per type, the space root is needed
	perType, err := NewPolicy(PolicyConfig{Default: "30d", SpaceTypes: map[string]string{storagespace.TypePersonal: "7d, 5"}})
	require.NoError(t, err)
	_, _, err = perType.CleanerFor(ctx, file)
	require.Error(t, err)
	_, _, err = perType.CleanerFor(ctx, nil)
	require.Error(t, err)
}

func TestPolicyEnabled(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config PolicyConfig
		want   bool
	}{
		{name: "nothing", config: PolicyConfig{}, want: false},
		{name: "all disabled", config: PolicyConfig{Default: "disabled", SpaceTypes: map[string]string{storagespace.TypeProject: "disabled"}}, want: false},
		{name: "a default", config: PolicyConfig{Default: "30d"}, want: true},
		{name: "only a type", config: PolicyConfig{SpaceTypes: map[string]string{storagespace.TypeProject: "90d"}}, want: true},
		{name: "a default, but every type disabled", config: PolicyConfig{Default: "30d", SpaceTypes: map[string]string{storagespace.TypePersonal: "disabled", storagespace.TypeProject: "disabled"}}, want: true},
		{name: "forced", config: PolicyConfig{Forced: "0"}, want: true},
		{name: "forced disabled", config: PolicyConfig{Default: "30d", Forced: "disabled"}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewPolicy(tc.config)
			require.NoError(t, err)
			require.Equal(t, tc.want, p.Enabled())
		})
	}

	var none *Policy
	require.False(t, none.Enabled())
	c, _, err := none.CleanerFor(context.Background(), nil)
	require.NoError(t, err)
	require.False(t, c.Enabled())
}

// An invalid strategy is reported with the setting it belongs to.
func TestNewPolicyNamesTheInvalidSetting(t *testing.T) {
	for _, tc := range []struct {
		config PolicyConfig
		want   string
	}{
		{config: PolicyConfig{Default: "whenever"}, want: "invalid default strategy"},
		{config: PolicyConfig{SpaceTypes: map[string]string{storagespace.TypeProject: "whenever"}}, want: "invalid strategy for project spaces"},
		{config: PolicyConfig{Forced: "whenever"}, want: "invalid forced strategy"},
	} {
		_, err := NewPolicy(tc.config)
		require.ErrorContains(t, err, tc.want)
	}
}

// String leaves out the empty settings and lists the types in a fixed order, so that it can
// tell whether two Policies have the same settings.
func TestPolicyString(t *testing.T) {
	p, err := NewPolicy(PolicyConfig{
		Default:    " 30d ",
		SpaceTypes: map[string]string{storagespace.TypeProject: "disabled", storagespace.TypePersonal: "7d, 5", "other": ""},
	})
	require.NoError(t, err)
	require.Equal(t, "default: 30d; personal: 7d, 5; project: disabled", p.String())

	p, err = NewPolicy(PolicyConfig{Forced: "0"})
	require.NoError(t, err)
	require.Equal(t, "default: disabled; forced: 0", p.String())

	var none *Policy
	require.Equal(t, "disabled", none.String())
}

func TestPolicyConfigFromOptions(t *testing.T) {
	c := PolicyConfigFromOptions(&options.Options{
		RevisionsCleanupStrategy:         "30d",
		RevisionsCleanupStrategyPersonal: "7d, 5",
		RevisionsCleanupStrategyProject:  "disabled",
	})
	require.Equal(t, PolicyConfig{
		Default:    "30d",
		SpaceTypes: map[string]string{storagespace.TypePersonal: "7d, 5", storagespace.TypeProject: "disabled"},
	}, c)
}
