// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/xattr"
)

var _ = Describe("readXattrs", func() {
	// get returns the values in values, and fails with the errors in failures
	get := func(values map[string]string, failures map[string]error) func(string, string) ([]byte, error) {
		return func(path, name string) ([]byte, error) {
			if err, ok := failures[name]; ok {
				return nil, &xattr.Error{Op: "xattr.get", Path: path, Name: name, Err: err}
			}
			return []byte(values[name]), nil
		}
	}

	It("reads all attributes", func() {
		attribs, err := readXattrs("/f", []string{"user.a", "user.b"},
			get(map[string]string{"user.a": "1", "user.b": "2"}, nil))
		Expect(err).ToNot(HaveOccurred())
		Expect(attribs).To(Equal(map[string][]byte{"user.a": []byte("1"), "user.b": []byte("2")}))
	})

	// https://github.com/opencloud-eu/reva/issues/1073: only the error of the last attribute
	// counted, so this returned no error and a nil map, which got cached
	It("fails when an attribute fails and the last one is read fine", func() {
		attribs, err := readXattrs("/f", []string{"user.a", "user.b", "user.c"},
			get(map[string]string{"user.a": "1", "user.c": "3"}, map[string]error{"user.b": syscall.ENOENT}))
		Expect(err).To(MatchError(ContainSubstring("failed to read 1 of 3 xattrs")))
		Expect(err).To(MatchError(syscall.ENOENT))
		Expect(attribs).To(BeNil())
	})

	It("fails with the first error when several attributes fail", func() {
		_, err := readXattrs("/f", []string{"user.a", "user.b", "user.c"},
			get(map[string]string{"user.c": "3"}, map[string]error{"user.a": syscall.EIO, "user.b": syscall.ENOENT}))
		Expect(err).To(MatchError(ContainSubstring("failed to read 2 of 3 xattrs")))
		Expect(err).To(MatchError(syscall.EIO))
	})

	It("doesn't fail for an attribute removed since it was listed", func() {
		attribs, err := readXattrs("/f", []string{"user.a", "user.gone"},
			get(map[string]string{"user.a": "1"}, map[string]error{"user.gone": xattr.ENOATTR}))
		Expect(err).ToNot(HaveOccurred())
		Expect(attribs).To(HaveKeyWithValue("user.a", []byte("1")))
	})
})
