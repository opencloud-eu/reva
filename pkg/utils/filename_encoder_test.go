package utils_test

import (
	"encoding/base64"
	"strings"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencloud-eu/reva/v2/pkg/utils"
)

var _ = Describe("FSSafeUserID", func() {
	DescribeTable("SafeFilename",
		func(userType userpb.UserType, opaqueID string, encoded bool) {
			id := &utils.FSSafeUserID{ID: &userpb.UserId{
				Type:     userType,
				OpaqueId: opaqueID,
			}}
			if encoded {
				Expect(id.SafeFilename()).To(Equal(base64.RawURLEncoding.EncodeToString([]byte(strings.ToLower(opaqueID)))))
			} else {
				Expect(id.SafeFilename()).To(Equal(opaqueID))
			}
		},
		Entry("preserves a primary user ID", userpb.UserType_USER_TYPE_PRIMARY, "MixedCase/ID", false),
		Entry("preserves another non-guest user ID", userpb.UserType_USER_TYPE_FEDERATED, "Federated@Example.COM", false),
		Entry("lowercases and encodes a guest user ID", userpb.UserType_USER_TYPE_GUEST, "Guest@Example.COM", true),
		Entry("uses URL-safe raw base64 for a guest user ID", userpb.UserType_USER_TYPE_GUEST, "???", true),
		Entry("encodes a file system path used as a guest user ID", userpb.UserType_USER_TYPE_GUEST, "../../etc/passwd", true),
		Entry("encodes a guest user ID with a quoted local part", userpb.UserType_USER_TYPE_GUEST, `"Guest.User"@Example.COM`, true),
		Entry("handles an empty primary user ID", userpb.UserType_USER_TYPE_PRIMARY, "", false),
		Entry("handles an empty guest user ID", userpb.UserType_USER_TYPE_GUEST, "", false),
	)

	Describe("Decode", func() {
		It("decodes a guest ID and preserves the wrapped ID", func() {
			original := &userpb.UserId{
				Type:     userpb.UserType_USER_TYPE_GUEST,
				OpaqueId: "original@example.com",
				Idp:      "idp.example.com",
				TenantId: "tenant",
			}

			decoded, err := (utils.FSSafeUserID{ID: original}).Decode(
				base64.RawURLEncoding.EncodeToString([]byte(strings.ToLower("../../etc/passwd"))),
			)

			Expect(err).NotTo(HaveOccurred())
			Expect(decoded.GetOpaqueId()).To(Equal("../../etc/passwd"))
			Expect(decoded.GetType()).To(Equal(userpb.UserType_USER_TYPE_GUEST))
			Expect(decoded.GetIdp()).To(Equal("idp.example.com"))
			Expect(decoded.GetTenantId()).To(Equal("tenant"))
			Expect(original.GetOpaqueId()).To(Equal("original@example.com"))
		})

		It("does not decode a non-guest ID", func() {
			id := utils.FSSafeUserID{ID: &userpb.UserId{Type: userpb.UserType_USER_TYPE_PRIMARY}}

			someb64 := base64.RawURLEncoding.EncodeToString([]byte("userid"))
			decoded, err := id.Decode(someb64)

			Expect(err).NotTo(HaveOccurred())
			Expect(decoded.GetOpaqueId()).To(Equal(someb64))
		})

		It("rejects malformed Base64 for a guest ID", func() {
			id := utils.FSSafeUserID{ID: &userpb.UserId{Type: userpb.UserType_USER_TYPE_GUEST}}

			decoded, err := id.Decode("not valid base64")

			Expect(err).To(HaveOccurred())
			Expect(decoded).To(BeNil())
		})
	})
})
