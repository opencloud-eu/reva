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

package tags

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddTags(t *testing.T) {
	testCases := []struct {
		Alias             string
		InitialTags       string
		TagsToAdd         string
		ExpectedTags      string
		ExpectNoTagsAdded bool
	}{
		{
			Alias:        "add one tag",
			InitialTags:  "",
			TagsToAdd:    "tag1",
			ExpectedTags: "tag1",
		},
		{
			Alias:        "add multiple tags",
			InitialTags:  "a,b,c",
			TagsToAdd:    "c,d,e",
			ExpectedTags: "d,e,a,b,c",
		},
		{
			Alias:             "no new tags",
			InitialTags:       "a,b,c,d,e,f",
			TagsToAdd:         "c,d,e",
			ExpectedTags:      "a,b,c,d,e,f",
			ExpectNoTagsAdded: true,
		},
		{
			Alias:        "ignore duplicate tags",
			InitialTags:  "a",
			TagsToAdd:    "b,b,b,b,a,a,a,a,a",
			ExpectedTags: "b,a",
		},
		{
			Alias:        "stop adding when maximum is reached",
			InitialTags:  "a,b,c,d,e,f,g,h,i",
			TagsToAdd:    "j,k,l,m",
			ExpectedTags: "j,a,b,c,d,e,f,g,h,i",
		},
		{
			Alias:             "don't do anything when already maxed",
			InitialTags:       "a,b,c,d,e,f,g,h,i,j",
			TagsToAdd:         "k,l,m,n,o,p",
			ExpectedTags:      "a,b,c,d,e,f,g,h,i,j",
			ExpectNoTagsAdded: true,
		},
		{
			Alias:        "trailing seps are ignored",
			InitialTags:  "tag1",
			TagsToAdd:    "tag2,",
			ExpectedTags: "tag2,tag1",
		},
		{
			Alias:        "special characters are allowed",
			InitialTags:  "old tag",
			TagsToAdd:    "new  tag,bettertag!",
			ExpectedTags: "new  tag,bettertag!,old tag",
		},
		{
			Alias:        "empty tags are ignored",
			InitialTags:  "tag1",
			TagsToAdd:    "tag2,,tag3",
			ExpectedTags: "tag2,tag3,tag1",
		},
		{
			Alias:             "empty tags are not ignored if there are no new tags",
			InitialTags:       "tag1",
			TagsToAdd:         ",,,tag1,,",
			ExpectedTags:      "tag1",
			ExpectNoTagsAdded: true,
		},
		{
			Alias:        "condition hold for initial tags too",
			InitialTags:  "tag1,tag1,,tag3,",
			TagsToAdd:    "tag2",
			ExpectedTags: "tag2,tag1,tag3",
		},
	}

	for _, tc := range testCases {
		ts := New(tc.InitialTags)
		ts.maxtags = 10

		added := ts.Add(tc.TagsToAdd)
		require.Equal(t, tc.ExpectNoTagsAdded, !added, tc.Alias)
		require.Equal(t, tc.ExpectedTags, ts.AsList(), tc.Alias)
		require.Equal(t, strings.Split(tc.ExpectedTags, ","), ts.AsSlice(), tc.Alias)
	}

}

func TestAddTagsSlices(t *testing.T) {
	testCases := []struct {
		Alias             string
		InitialTags       []string
		TagsToAdd         []string
		ExpectedTags      string
		ExpectNoTagsAdded bool
	}{
		{
			Alias:        "add one tag",
			InitialTags:  []string{},
			TagsToAdd:    []string{"tag1"},
			ExpectedTags: "tag1",
		},
		{
			Alias:        "add multiple tags",
			InitialTags:  []string{"a", "b", "c"},
			TagsToAdd:    []string{"c,d,e"},
			ExpectedTags: "d,e,a,b,c",
		},
		{
			Alias:             "no new tags",
			InitialTags:       []string{"a", "b", "c", "d", "e", "f"},
			TagsToAdd:         []string{"c", "d", "e"},
			ExpectedTags:      "a,b,c,d,e,f",
			ExpectNoTagsAdded: true,
		},
		{
			Alias:        "ignore duplicate tags",
			InitialTags:  []string{"a"},
			TagsToAdd:    []string{"b", "b", "b", "b", "a", "a", "a", "a", "a"},
			ExpectedTags: "b,a",
		},
		{
			Alias:        "stop adding when maximum is reached",
			InitialTags:  []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"},
			TagsToAdd:    []string{"j", "k", "l", "m"},
			ExpectedTags: "j,a,b,c,d,e,f,g,h,i",
		},
		{
			Alias:             "don't do anything when already maxed",
			InitialTags:       []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"},
			TagsToAdd:         []string{"k", "l", "m", "n", "o", "p"},
			ExpectedTags:      "a,b,c,d,e,f,g,h,i,j",
			ExpectNoTagsAdded: true,
		},
		{
			Alias:        "special characters are allowed",
			InitialTags:  []string{"old tag"},
			TagsToAdd:    []string{"new  tag", "bettertag!"},
			ExpectedTags: "new  tag,bettertag!,old tag",
		},
		{
			Alias:        "empty tags are ignored",
			InitialTags:  []string{"tag1"},
			TagsToAdd:    []string{"tag2", "", "tag3"},
			ExpectedTags: "tag2,tag3,tag1",
		},
		{
			Alias:             "empty tags are not ignored if there are no new tags",
			InitialTags:       []string{"tag1"},
			TagsToAdd:         []string{"", "", "", "tag1", "", ""},
			ExpectedTags:      "tag1",
			ExpectNoTagsAdded: true,
		},
		{
			Alias:        "condition hold for initial tags too",
			InitialTags:  []string{"tag1", "tag1", "", "tag3", ""},
			TagsToAdd:    []string{"tag2"},
			ExpectedTags: "tag2,tag1,tag3",
		},
	}

	for _, tc := range testCases {
		ts := New(tc.InitialTags...)
		ts.maxtags = 10

		added := ts.Add(tc.TagsToAdd...)
		require.Equal(t, tc.ExpectNoTagsAdded, !added, tc.Alias)
		require.Equal(t, tc.ExpectedTags, ts.AsList(), tc.Alias)
		require.Equal(t, strings.Split(tc.ExpectedTags, ","), ts.AsSlice(), tc.Alias)
	}

}

func TestRemoveTags(t *testing.T) {
	testCases := []struct {
		Alias               string
		InitialTags         string
		TagsToRemove        string
		ExpectedTags        string
		ExpectNoTagsRemoved bool
	}{
		{
			Alias:        "simple",
			InitialTags:  "a,b,c",
			TagsToRemove: "a,b",
			ExpectedTags: "c",
		},
		{
			Alias:        "remove all tags",
			InitialTags:  "a,b,c,d,e,f",
			TagsToRemove: "f,c,a,d,e,b",
			ExpectedTags: "",
		},
		{
			Alias:        "ignore duplicate tags",
			InitialTags:  "a,b",
			TagsToRemove: "b,b,b,b",
			ExpectedTags: "a",
		},
		{
			Alias:        "order of tags is preserved",
			InitialTags:  "a,b,c,d",
			TagsToRemove: "a,c",
			ExpectedTags: "b,d",
		},
		{
			Alias:        "special characters are allowed",
			InitialTags:  "anothertag,btag!!,c#,distro 66",
			TagsToRemove: "distro 66,btag!!",
			ExpectedTags: "anothertag,c#",
		},
		{
			Alias:               "empty list errors",
			InitialTags:         "tag1,tag2",
			TagsToRemove:        ",,,,,",
			ExpectedTags:        "tag1,tag2",
			ExpectNoTagsRemoved: true,
		},
		{
			Alias:               "unknown tag errors",
			InitialTags:         "tag1,tag2",
			TagsToRemove:        "tag3",
			ExpectedTags:        "tag1,tag2",
			ExpectNoTagsRemoved: true,
		},
	}

	for _, tc := range testCases {
		ts := New(tc.InitialTags)

		removed := ts.Remove(tc.TagsToRemove)
		require.Equal(t, tc.ExpectNoTagsRemoved, !removed, tc.Alias)
		require.Equal(t, tc.ExpectedTags, ts.AsList(), tc.Alias)
	}

}

func TestRemoveTagsSlices(t *testing.T) {
	testCases := []struct {
		Alias               string
		InitialTags         []string
		TagsToRemove        []string
		ExpectedTags        string
		ExpectNoTagsRemoved bool
	}{
		{
			Alias:        "simple",
			InitialTags:  []string{"a", "b", "c"},
			TagsToRemove: []string{"a", "b"},
			ExpectedTags: "c",
		},
		{
			Alias:        "remove all tags",
			InitialTags:  []string{"a", "b", "c", "d", "e", "f"},
			TagsToRemove: []string{"f", "c", "a", "d", "e", "b"},
			ExpectedTags: "",
		},
		{
			Alias:        "ignore duplicate tags",
			InitialTags:  []string{"a", "b"},
			TagsToRemove: []string{"b", "b", "b", "b"},
			ExpectedTags: "a",
		},
		{
			Alias:        "order of tags is preserved",
			InitialTags:  []string{"a", "b", "c", "d"},
			TagsToRemove: []string{"a", "c"},
			ExpectedTags: "b,d",
		},
		{
			Alias:        "special characters are allowed",
			InitialTags:  []string{"anothertag", "btag!!", "c#", "distro 66"},
			TagsToRemove: []string{"distro 66", "btag!!"},
			ExpectedTags: "anothertag,c#",
		},
		{
			Alias:               "empty list errors",
			InitialTags:         []string{"tag1", "tag2"},
			TagsToRemove:        []string{"", "", "", "", "", ""},
			ExpectedTags:        "tag1,tag2",
			ExpectNoTagsRemoved: true,
		},
		{
			Alias:               "unknown tag errors",
			InitialTags:         []string{"tag1", "tag2"},
			TagsToRemove:        []string{"tag3"},
			ExpectedTags:        "tag1,tag2",
			ExpectNoTagsRemoved: true,
		},
	}

	for _, tc := range testCases {
		ts := New(tc.InitialTags...)

		removed := ts.Remove(tc.TagsToRemove...)
		require.Equal(t, tc.ExpectNoTagsRemoved, !removed, tc.Alias)
		require.Equal(t, tc.ExpectedTags, ts.AsList(), tc.Alias)
	}

}

func TestBuilders(t *testing.T) {
	testCases := []struct {
		Alias string
		List  string
		Slice []string
	}{
		{
			Alias: "simple",
			List:  "a,b,c",
			Slice: []string{"a", "b", "c"},
		},
		{
			Alias: "zero values",
			List:  "",
			Slice: nil,
		},
	}

	for _, tc := range testCases {
		list := New(tc.List)
		slice := New(tc.Slice...)

		require.Equal(t, list.AsSlice(), slice.AsSlice())
		require.Equal(t, list.AsList(), slice.AsList())
	}
}

func TestAddAfterRemoveAtMaximum(t *testing.T) {
	full := []string{"a", "b", "c", "d", "e"}

	testCases := []struct {
		Alias        string
		TagsToRemove []string
		TagsToAdd    []string
		ExpectedTags string
		ExpectAdded  bool
	}{
		{
			Alias:        "a removed tag frees a slot",
			TagsToRemove: []string{"a"},
			TagsToAdd:    []string{"f"},
			ExpectedTags: "f,b,c,d,e",
			ExpectAdded:  true,
		},
		{
			Alias:        "as many slots as removed tags",
			TagsToRemove: []string{"a", "b"},
			TagsToAdd:    []string{"f", "g", "h"},
			ExpectedTags: "f,g,c,d,e",
			ExpectAdded:  true,
		},
		{
			Alias:        "removing a removed tag again frees nothing",
			TagsToRemove: []string{"a", "a"},
			TagsToAdd:    []string{"f", "g"},
			ExpectedTags: "f,b,c,d,e",
			ExpectAdded:  true,
		},
		{
			Alias:        "removing an unknown tag frees nothing",
			TagsToRemove: []string{"x"},
			TagsToAdd:    []string{"f"},
			ExpectedTags: "a,b,c,d,e",
			ExpectAdded:  false,
		},
		{
			Alias:        "a removed tag can be added again",
			TagsToRemove: []string{"c"},
			TagsToAdd:    []string{"c"},
			ExpectedTags: "c,a,b,d,e",
			ExpectAdded:  true,
		},
	}

	for _, tc := range testCases {
		ts := New(full...)
		ts.maxtags = len(full)

		ts.Remove(tc.TagsToRemove...)
		added := ts.Add(tc.TagsToAdd...)
		require.Equal(t, tc.ExpectAdded, added, tc.Alias)
		require.Equal(t, tc.ExpectedTags, ts.AsList(), tc.Alias)
	}
}

func TestNormalize(t *testing.T) {
	testCases := []struct {
		Alias    string
		Input    []string
		Expected []string
	}{
		{
			Alias:    "one tag stays as it is",
			Input:    []string{"tag1"},
			Expected: []string{"tag1"},
		},
		{
			Alias:    "a list is split at the separator",
			Input:    []string{"a,b,c"},
			Expected: []string{"a", "b", "c"},
		},
		{
			Alias:    "lists and single tags can be mixed",
			Input:    []string{"a,b", "c", "d,e"},
			Expected: []string{"a", "b", "c", "d", "e"},
		},
		{
			Alias:    "whitespace around tags is removed",
			Input:    []string{" a ,\tb", "c\n", "  d"},
			Expected: []string{"a", "b", "c", "d"},
		},
		{
			// rarely typed, but easy to slip into a tag to make it look like another one
			Alias: "unusual unicode whitespace around tags is removed",
			Input: []string{
				"\u00a0a\u00a0",   // no-break space
				"\u2003b\u2009",   // em space, thin space
				"\u3000c\u202f",   // ideographic space, narrow no-break space
				"\u2028d\u0085",   // line separator, next line
				"\u00a0,\u3000,e", // only unusual whitespace between separators
			},
			Expected: []string{"a", "b", "c", "d", "e"},
		},
		{
			Alias: "invisible zero-width characters around tags are removed",
			Input: []string{
				"\u200ba\u200b",   // zero-width space
				"\u2060b\ufeff",   // word joiner, zero-width no-break space (BOM)
				"\u200b c \u00a0", // mixed with whitespace
				"\u200b,\ufeff,d", // only invisible characters between separators
			},
			Expected: []string{"a", "b", "c", "d"},
		},
		{
			Alias:    "zero-width characters inside a tag are kept",
			Input:    []string{"wo\u200brk", "\U0001F469\u200d\U0001F4BB"}, // the second one is the emoji "woman technologist"
			Expected: []string{"wo\u200brk", "\U0001F469\u200d\U0001F4BB"},
		},
		{
			Alias:    "whitespace inside a tag is kept",
			Input:    []string{" new  tag , distro 66"},
			Expected: []string{"new  tag", "distro 66"},
		},
		{
			Alias:    "empty and blank tags are dropped",
			Input:    []string{"a,,b,", "", " ", ", ,"},
			Expected: []string{"a", "b"},
		},
		{
			Alias:    "duplicates are kept, deduplication is up to the caller",
			Input:    []string{"a,a", " a"},
			Expected: []string{"a", "a", "a"},
		},
		{
			Alias:    "no input",
			Input:    nil,
			Expected: []string{},
		},
	}

	for _, tc := range testCases {
		require.Equal(t, tc.Expected, New().normalize(tc.Input), tc.Alias)
	}
}

func TestWhitespaceAroundTags(t *testing.T) {
	t.Run("stored tags are trimmed", func(t *testing.T) {
		ts := New(" a , b", "c ")
		require.Equal(t, "a,b,c", ts.AsList())
	})

	t.Run("adding a padded tag that exists adds nothing", func(t *testing.T) {
		ts := New("a,b")
		require.False(t, ts.Add(" a", "b "))
		require.Equal(t, "a,b", ts.AsList())
	})

	t.Run("added tags are trimmed", func(t *testing.T) {
		ts := New("a")
		require.True(t, ts.Add(" b ,  c"))
		require.Equal(t, []string{"b", "c", "a"}, ts.AsSlice())
	})

	t.Run("a padded tag removes the stored tag", func(t *testing.T) {
		ts := New("a,b,c")
		require.True(t, ts.Remove(" b "))
		require.Equal(t, "a,c", ts.AsList())
	})

	t.Run("a stored padded tag can be removed without padding", func(t *testing.T) {
		ts := New(" a ,b")
		require.True(t, ts.Remove("a"))
		require.Equal(t, "b", ts.AsList())
	})

	t.Run("removing only blank tags removes nothing", func(t *testing.T) {
		ts := New("a,b")
		require.False(t, ts.Remove(" ", " , "))
		require.Equal(t, "a,b", ts.AsList())
	})
}

func TestMaxLengthValidator(t *testing.T) {
	testCases := []struct {
		Alias     string
		MaxLength int
		Tags      []string
		Errors    []string // substrings the error must contain, nil if no error is expected
	}{
		{Alias: "no tags", MaxLength: 3, Tags: nil},
		{Alias: "tags up to the limit", MaxLength: 3, Tags: []string{"a", "abc"}},
		{Alias: "a tag over the limit", MaxLength: 3, Tags: []string{"abc", "abcd"}, Errors: []string{"3 characters", `"abcd"`}},
		{Alias: "all tags over the limit are listed", MaxLength: 3, Tags: []string{"abcd", "ok", "wxyz"}, Errors: []string{`"abcd"`, `"wxyz"`}},
		{Alias: "characters are counted, not bytes", MaxLength: 3, Tags: []string{"äöü", "日本語"}},
		{Alias: "multi-byte characters over the limit", MaxLength: 3, Tags: []string{"äöüß"}, Errors: []string{`"äöüß"`}},
		{Alias: "invalid UTF-8", MaxLength: 3, Tags: []string{"a\xffb"}, Errors: []string{"UTF-8"}},
		{Alias: "no limit", MaxLength: 0, Tags: []string{strings.Repeat("a", 1000)}},
		{Alias: "negative limit means no limit", MaxLength: -1, Tags: []string{strings.Repeat("a", 1000)}},
		{Alias: "invalid UTF-8 without a limit", MaxLength: 0, Tags: []string{"\xff"}, Errors: []string{"UTF-8"}},
	}

	for _, tc := range testCases {
		t.Run(tc.Alias, func(t *testing.T) {
			err := MaxLengthValidator(tc.MaxLength)(tc.Tags)
			if tc.Errors == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, e := range tc.Errors {
				require.Contains(t, err.Error(), e)
			}
			require.NotContains(t, err.Error(), `"ok"`)
		})
	}
}

func TestAddValidated(t *testing.T) {
	t.Run("valid tags are added", func(t *testing.T) {
		ts := New("a")
		added, err := ts.AddValidated(MaxLengthValidator(3), "bc", "def")
		require.NoError(t, err)
		require.True(t, added)
		require.Equal(t, "bc,def,a", ts.AsList())
	})

	t.Run("one invalid tag rejects all of them", func(t *testing.T) {
		ts := New("a")
		added, err := ts.AddValidated(MaxLengthValidator(3), "bc", "toolong")
		require.Error(t, err)
		require.False(t, added)
		require.Equal(t, "a", ts.AsList())
	})

	t.Run("the validator gets normalized tags", func(t *testing.T) {
		var got []string
		ts := New()
		added, err := ts.AddValidated(func(tags []string) error {
			got = tags
			return nil
		}, " a , b", "", "c ")
		require.NoError(t, err)
		require.True(t, added)
		require.Equal(t, []string{"a", "b", "c"}, got)
	})

	t.Run("whitespace doesn't count towards the length", func(t *testing.T) {
		ts := New()
		added, err := ts.AddValidated(MaxLengthValidator(3), "  abc  ")
		require.NoError(t, err)
		require.True(t, added)
		require.Equal(t, "abc", ts.AsList())
	})

	t.Run("existing tags are not added again", func(t *testing.T) {
		ts := New("a,b")
		added, err := ts.AddValidated(MaxLengthValidator(3), " a", "b ")
		require.NoError(t, err)
		require.False(t, added)
		require.Equal(t, "a,b", ts.AsList())
	})

	t.Run("the validator error is returned as is", func(t *testing.T) {
		errInvalid := errors.New("invalid")
		ts := New("a")
		added, err := ts.AddValidated(func([]string) error { return errInvalid }, "b")
		require.ErrorIs(t, err, errInvalid)
		require.False(t, added)
		require.Equal(t, "a", ts.AsList())
	})
}
