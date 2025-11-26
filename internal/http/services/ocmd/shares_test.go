package ocmd

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Shares", func() {
	Describe("getLocalUserID", func() {
		It("fails when user is not in the format user@idp@provider", func() {
			user := "foo"
			_, _, err := getLocalUserID(user)
			Expect(err).To(HaveOccurred())
		})
		It("fails when user is missing", func() {
			user := "@example.org"
			_, _, err := getLocalUserID(user)
			Expect(err).To(HaveOccurred())
		})

		It("returns the local user id", func() {
			user := "user@idp@provider"
			id, provider, err := getLocalUserID(user)
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal("user"))
			Expect(provider).To(Equal("provider"))
		})
		It("returns the local user id without provider", func() {
			user := "user@provider"
			id, provider, err := getLocalUserID(user)
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal("user"))
			Expect(provider).To(Equal("provider"))
		})
		It("contains a uuid as user and a provider with protocol", func() {
			user := "4c510ada-c86b-4815-8820-42cdf82c3d51@https://cernbox.cern.ch"
			id, provider, err := getLocalUserID(user)
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal("4c510ada-c86b-4815-8820-42cdf82c3d51"))
			Expect(provider).To(Equal("https://cernbox.cern.ch"))
		})
	})

	Describe("getIDAndMeshProvider", func() {
		It("fails when user is not in the format user@provider", func() {
			user := "foo"
			_, _, err := getIDAndMeshProvider(user)
			Expect(err).To(HaveOccurred())
		})

		It("fails when user is in the format user@provider with no provider", func() {
			user := "foo@"
			_, _, err := getIDAndMeshProvider(user)
			Expect(err).To(HaveOccurred())
		})

		It("fails when the user is missing", func() {
			user := "@example.org"
			_, _, err := getIDAndMeshProvider(user)
			Expect(err).To(HaveOccurred())
		})

		It("returns the user and provider", func() {
			user := "alice@eos"
			id, provider, err := getIDAndMeshProvider(user)
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal("alice"))
			Expect(provider).To(Equal("eos"))
		})
		It("returns a e-mail address as user and provider", func() {
			user := "foo@bar.com@example.com"
			id, provider, err := getIDAndMeshProvider(user)
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal("foo@bar.com"))
			Expect(provider).To(Equal("example.com"))
		})

		It("contains a matrix id as user", func() {
			user := "@alice:matrix.example.org@example.org"
			id, provider, err := getIDAndMeshProvider(user)
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal("@alice:matrix.example.org"))
			Expect(provider).To(Equal("example.org"))
		})

		It("contains a uuid as user and a provider with protocol", func() {
			user := "4c510ada-c86b-4815-8820-42cdf82c3d51@https://cernbox.cern.ch"
			id, provider, err := getIDAndMeshProvider(user)
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal("4c510ada-c86b-4815-8820-42cdf82c3d51"))
			Expect(provider).To(Equal("https://cernbox.cern.ch"))
		})
	})
})
