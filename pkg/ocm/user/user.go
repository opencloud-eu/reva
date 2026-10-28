package user

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
)

// LocalUserFederatedID creates a federated id for local users by
// 1. stripping the protocol from the domain and
// 2. if the domain is different from the idp, add the idp to the opaque id
func LocalUserFederatedID(id *userpb.UserId, domain string) *userpb.UserId {
	if u, err := url.Parse(domain); err == nil && u.Host != "" {
		domain = u.Host
	}
	u := &userpb.UserId{
		Type:     userpb.UserType_USER_TYPE_FEDERATED,
		Idp:      id.GetIdp(),
		OpaqueId: id.GetOpaqueId(),
	}

	if domain != "" && id.GetIdp() != domain {
		if id.GetIdp() != "" {
			u.OpaqueId = id.GetOpaqueId() + "@" + id.GetIdp()
		}
		u.Idp = domain
	}
	return u
}

// DecodeRemoteUserFederatedID decodes opaque id into remote user's federated id by
// splitting the opaque id at the last @ to get the opaque id and the domain
func DecodeRemoteUserFederatedID(id *userpb.UserId) *userpb.UserId {
	remoteId := &userpb.UserId{
		Type:     userpb.UserType_USER_TYPE_PRIMARY,
		Idp:      id.Idp,
		OpaqueId: id.OpaqueId,
	}
	remote := id.OpaqueId
	last := strings.LastIndex(remote, "@")
	if last == -1 {
		return remoteId
	}
	remoteId.OpaqueId = remote[:last]
	remoteId.Idp = remote[last+1:]

	return remoteId
}

// ParseOCMAddress parses an OCM address in the form <id>@<provider> according to
// the OCM specification. The provider must not include a URI scheme.
func ParseOCMAddress(user string) (*userpb.UserId, error) {
	last := strings.LastIndex(user, "@")
	if last == -1 {
		return nil, errors.New("not in the form <id>@<provider>")
	}

	id, idp := user[:last], user[last+1:]
	if id == "" {
		return nil, errors.New("id cannot be empty")
	}
	if idp == "" {
		return nil, errors.New("provider cannot be empty")
	}
	idp = TrimOCMScheme(idp)

	return &userpb.UserId{
		OpaqueId: id,
		Idp:      idp,
		Type:     userpb.UserType_USER_TYPE_FEDERATED,
	}, nil
}

// TrimOCMScheme removes a leading http(s):// scheme from an OCM host string.
// OCM Addresses are not URIs and MUST NOT carry a scheme, but some servers
// (Nextcloud, oCIS, OpenCloud) include one; we strip it defensively.
func TrimOCMScheme(host string) string {
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	return host
}

// NormalizeRemoteUserID returns the bare OCM identifier for a remote user.
//
// Per the OCM spec the invite userID MUST be the bare identifier of the user
// at their OCM Server, and the host travels separately in recipientProvider.
// Some non-conformant servers append the host to userID anyway (oCIS sends
// "id@host", OpenCloud sends "id@https://host"). If stored verbatim, the
// qualified string is kept as OpaqueId and later re-appended when building
// shareWith, producing "id@host@host" (or with a scheme).
//
// We strip a trailing "@<provider>" suffix ONLY when it matches the already-known
// provider domain, repeating to collapse accidental double-qualification.
func NormalizeRemoteUserID(userID, providerDomain string) string {
	host := TrimOCMScheme(providerDomain)
	if host == "" {
		return userID
	}
	for {
		uid, err := ParseOCMAddress(userID)
		if err != nil || !strings.EqualFold(TrimOCMScheme(uid.Idp), host) {
			return userID
		}
		userID = uid.OpaqueId
	}
}

// CanonicalizeRemoteUserID normalizes a federated remote user id in place.
func CanonicalizeRemoteUserID(id *userpb.UserId) {
	if id == nil {
		return
	}
	id.Idp = TrimOCMScheme(id.Idp)
	id.OpaqueId = NormalizeRemoteUserID(id.OpaqueId, id.Idp)
}

// IdpsEqual reports whether two OCM provider strings refer to the same host,
// ignoring URI schemes and case.
func IdpsEqual(idp1, idp2 string) bool {
	normalizeIDP := func(s string) (string, error) {
		u, err := url.Parse(s)
		if err != nil {
			return "", errors.New("could not parse url")
		}

		if u.Scheme == "" {
			return strings.ToLower(u.Path), nil
		}
		return strings.ToLower(u.Hostname()), nil
	}

	domain1, err := normalizeIDP(idp1)
	if err != nil {
		return false
	}
	domain2, err := normalizeIDP(idp2)
	if err != nil {
		return false
	}

	return domain1 == domain2
}

// RemoteUserIDsMatch reports whether two federated user ids refer to the same
// remote user, including legacy encodings stored before normalization on write.
func RemoteUserIDsMatch(stored, query *userpb.UserId) bool {
	if stored == nil || query == nil {
		return false
	}

	if stored.GetOpaqueId() == query.GetOpaqueId() {
		if query.GetIdp() == "" || IdpsEqual(stored.GetIdp(), query.GetIdp()) {
			return true
		}
	}

	if query.GetIdp() != "" && !IdpsEqual(stored.GetIdp(), query.GetIdp()) {
		return false
	}

	storedHost := TrimOCMScheme(stored.GetIdp())
	queryHost := TrimOCMScheme(query.GetIdp())
	storedBare := NormalizeRemoteUserID(stored.GetOpaqueId(), storedHost)
	queryBare := NormalizeRemoteUserID(query.GetOpaqueId(), queryHost)

	return storedBare == queryBare
}

// FormatOCMUser renders a CS3 user id as an OCM Address "<opaque-id>@<host>".
// It strips any scheme from the host and collapses a redundant, self-referential
// provider suffix already present in the opaque id.
func FormatOCMUser(u *userpb.UserId) string {
	if u.GetIdp() == "" {
		return u.GetOpaqueId()
	}
	host := TrimOCMScheme(u.Idp)
	opaque := NormalizeRemoteUserID(u.OpaqueId, host)
	return fmt.Sprintf("%s@%s", opaque, host)
}
