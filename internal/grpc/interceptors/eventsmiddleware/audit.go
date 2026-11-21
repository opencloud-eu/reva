package eventsmiddleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path"
	"strings"
	"sync"
	"time"

	userpb "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	collaboration "github.com/cs3org/go-cs3apis/cs3/sharing/collaboration/v1beta1"
	link "github.com/cs3org/go-cs3apis/cs3/sharing/link/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/go-viper/mapstructure/v2"
	"github.com/opencloud-eu/reva/v3/pkg/audit"
	ctxpkg "github.com/opencloud-eu/reva/v3/pkg/ctx"
	"github.com/opencloud-eu/reva/v3/pkg/events/raw"
	"github.com/opencloud-eu/reva/v3/pkg/storagespace"
	"github.com/opencloud-eu/reva/v3/pkg/utils"
	"go.opentelemetry.io/otel/trace"
)

// auditConfig is the audit part of the middleware config.
type auditConfig struct {
	Enabled bool   `mapstructure:"audit_enabled"`
	Mode    string `mapstructure:"audit_mode"`
	Service string `mapstructure:"audit_service"`
	Version string `mapstructure:"audit_service_version"`
}

// emitterFromConfig returns the audit emitter, a discarding one when audit is off.
func emitterFromConfig(m map[string]interface{}) (audit.Emitter, error) {
	var ac auditConfig
	if err := mapstructure.Decode(m, &ac); err != nil {
		return nil, err
	}
	if !ac.Enabled {
		return audit.Discard{}, nil
	}
	var nc raw.Config
	if err := mapstructure.Decode(m, &nc); err != nil {
		return nil, err
	}
	if nc.Endpoint == "" {
		return nil, audit.ErrNoEndpoint
	}
	name, _ := m["name"].(string)
	return audit.NewNatsEmitter(context.Background(), name+"-audit", nc, audit.Options{
		Service: audit.Service{Name: ac.Service, Version: ac.Version},
		Mode:    audit.Mode(ac.Mode),
	})
}

// outcome maps a CS3 status to the audit outcome and reason.
func outcome(s *rpc.Status) (audit.Outcome, string) {
	switch s.GetCode() {
	case rpc.Code_CODE_OK:
		return audit.OutcomeSuccess, ""
	case rpc.Code_CODE_PERMISSION_DENIED:
		return audit.OutcomeDenied, "permission_denied"
	case rpc.Code_CODE_UNAUTHENTICATED:
		return audit.OutcomeDenied, "unauthenticated"
	default:
		return audit.OutcomeFailure, strings.ToLower(strings.TrimPrefix(s.GetCode().String(), "CODE_"))
	}
}

// auditEvent turns a CS3 call into an audit event. item is filled by the
// storage driver with the type, ID and name of the item the call acts on. The
// type is needed for move, delete, restore and purge; when it is unknown,
// e.g. because the item doesn't exist, no event is emitted for these calls.
//
//nolint:gocyclo
func auditEvent(req, res interface{}, item *storagespace.ItemTypeSlot) (audit.Event, bool) {
	if item == nil {
		item = &storagespace.ItemTypeSlot{}
	}
	itemType := item.Type
	var ev audit.Event
	switch v := res.(type) {
	case *provider.MoveResponse:
		r := req.(*provider.MoveRequest)
		src, dst := r.GetSource(), r.GetDestination()
		renamed := sameParent(src, dst)
		d := pick(itemType, audit.FileMove, audit.FolderMove)
		if renamed {
			d = pick(itemType, audit.FileRename, audit.FolderRename)
		}
		if d == nil {
			return ev, false
		}
		ev = audit.Event{Definition: d, Target: refTarget(src, item)}
		before, after := movePaths(src, dst, item)
		if before != "" {
			ev.Change = &audit.Change{Before: map[string]any{"path": before}}
		}
		if after != "" {
			if ev.Change == nil {
				ev.Change = &audit.Change{}
			}
			ev.Change.After = map[string]any{"path": after}
		}
	case *provider.DeleteResponse:
		d := pick(itemType, audit.FileDelete, audit.FolderDelete)
		if d == nil {
			return ev, false
		}
		ev = audit.Event{Definition: d, Target: refTarget(req.(*provider.DeleteRequest).GetRef(), item)}
	case *provider.RestoreRecycleItemResponse:
		d := pick(itemType, audit.FileRestore, audit.FolderRestore)
		if d == nil {
			return ev, false
		}
		r := req.(*provider.RestoreRecycleItemRequest)
		ev = audit.Event{Definition: d, Target: trashTarget(r.GetRef(), r.GetKey(), item)}
	case *provider.CreateContainerResponse:
		r := req.(*provider.CreateContainerRequest)
		ev = audit.Event{Definition: audit.FolderCreate, Target: refTarget(r.GetRef(), item)}
	case *provider.SetLockResponse:
		ev = audit.Event{Definition: audit.FileLock, Target: refTarget(req.(*provider.SetLockRequest).GetRef(), item)}
	case *provider.UnlockResponse:
		ev = audit.Event{Definition: audit.FileUnlock, Target: refTarget(req.(*provider.UnlockRequest).GetRef(), item)}
	case *provider.RestoreFileVersionResponse:
		ev = audit.Event{Definition: audit.FileVersionRestore, Target: refTarget(req.(*provider.RestoreFileVersionRequest).GetRef(), item)}
	case *provider.PurgeRecycleResponse:
		r := req.(*provider.PurgeRecycleRequest)
		id := r.GetRef().GetResourceId()
		if id == nil || id.GetOpaqueId() != id.GetSpaceId() || r.GetKey() != "" {
			d := pick(itemType, audit.FilePurge, audit.FolderPurge)
			if d == nil {
				return ev, false
			}
			ev = audit.Event{Definition: d, Target: trashTarget(r.GetRef(), r.GetKey(), item)}
			break
		}
		ev = audit.Event{Definition: audit.TrashPurgeAll, Target: spaceTarget(id)}

	case *collaboration.CreateShareResponse:
		if utils.ExistsInOpaque(v.GetOpaque(), "noevent") {
			return ev, false
		}
		r := req.(*collaboration.CreateShareRequest)
		rid := r.GetResourceInfo().GetId()
		// the sharing service marks space grants in the response; a failed
		// space grant therefore shows up as a failed share.create
		if utils.ExistsInOpaque(v.GetOpaque(), "spacegrant") {
			ev = audit.Event{Definition: audit.SpaceMemberAdd, Target: spaceTarget(rid)}
			ev.Target.Name = utils.ReadPlainFromOpaque(v.GetOpaque(), "resourcename")
		} else {
			ev = audit.Event{Definition: audit.ShareCreate, Target: shareTarget(v.GetShare().GetId().GetOpaqueId(), rid, r.GetResourceInfo().GetName())}
		}
		ev.Change = shareCreated(r, v)
	case *collaboration.UpdateShareResponse:
		if utils.ExistsInOpaque(v.GetOpaque(), "spacegrant") {
			ev = audit.Event{Definition: audit.SpaceMemberUpdate, Target: spaceTarget(v.GetShare().GetResourceId())}
			ev.Target.Name = utils.ReadPlainFromOpaque(v.GetOpaque(), "resourcename")
		} else {
			r := req.(*collaboration.UpdateShareRequest)
			ev = audit.Event{Definition: audit.ShareUpdate, Target: shareTarget(shareID(r.GetShare().GetId(), r.GetRef()), v.GetShare().GetResourceId(), utils.ReadPlainFromOpaque(v.GetOpaque(), "resourcename"))}
		}
		ev.Change = shareUpdated(req.(*collaboration.UpdateShareRequest), v)
	case *collaboration.RemoveShareResponse:
		r := req.(*collaboration.RemoveShareRequest)
		var rid *provider.ResourceId
		_ = utils.ReadJSONFromOpaque(v.GetOpaque(), "resourceid", &rid)
		if utils.ExistsInOpaque(v.GetOpaque(), "spacegrant") {
			ev = audit.Event{Definition: audit.SpaceMemberRemove, Target: spaceTarget(rid)}
			ev.Target.Name = utils.ReadPlainFromOpaque(v.GetOpaque(), "resourcename")
		} else {
			ev = audit.Event{Definition: audit.ShareDelete, Target: shareTarget(shareID(nil, r.GetRef()), rid, utils.ReadPlainFromOpaque(v.GetOpaque(), "resourcename"))}
		}
		ev.Change = shareRemoved(v.GetOpaque())
	case *collaboration.UpdateReceivedShareResponse:
		rs := v.GetShare()
		switch rs.GetState() {
		case collaboration.ShareState_SHARE_STATE_ACCEPTED:
			ev = audit.Event{Definition: audit.ShareAccept}
		case collaboration.ShareState_SHARE_STATE_REJECTED:
			ev = audit.Event{Definition: audit.ShareDecline}
		default:
			return ev, false
		}
		ev.Target = shareTarget(rs.GetShare().GetId().GetOpaqueId(), rs.GetShare().GetResourceId(), path.Base(rs.GetMountPoint().GetPath()))
		if ev.Target.Name == "." || ev.Target.Name == "/" {
			ev.Target.Name = ""
		}

	case *link.CreatePublicShareResponse:
		r := req.(*link.CreatePublicShareRequest)
		ev = audit.Event{Definition: audit.PublicLinkCreate, Target: shareTarget(v.GetShare().GetId().GetOpaqueId(), r.GetResourceInfo().GetId(), r.GetResourceInfo().GetName())}
		ev.Target.Type = "public_link"
		ev.Change = linkCreated(r, v)
	case *link.UpdatePublicShareResponse:
		r := req.(*link.UpdatePublicShareRequest)
		ev = audit.Event{Definition: audit.PublicLinkUpdate, Target: shareTarget(r.GetRef().GetId().GetOpaqueId(), v.GetShare().GetResourceId(), utils.ReadPlainFromOpaque(v.GetOpaque(), "resourcename"))}
		ev.Change = linkUpdated(r, v)
	case *link.GetPublicShareByTokenResponse:
		return publicLinkAccess(req.(*link.GetPublicShareByTokenRequest), v)
	case *link.RemovePublicShareResponse:
		r := req.(*link.RemovePublicShareRequest)
		var rid *provider.ResourceId
		_ = utils.ReadJSONFromOpaque(v.GetOpaque(), "resourceid", &rid)
		ev = audit.Event{Definition: audit.PublicLinkDelete, Target: shareTarget(r.GetRef().GetId().GetOpaqueId(), rid, utils.ReadPlainFromOpaque(v.GetOpaque(), "resourcename"))}
		ev.Change = linkRemoved(v.GetOpaque())

	case *provider.CreateStorageSpaceResponse:
		r := req.(*provider.CreateStorageSpaceRequest)
		// the proxy calls CreateHome on every request, for an existing
		// personal space nothing was created
		if r.GetType() == "personal" && v.GetStatus().GetCode() == rpc.Code_CODE_ALREADY_EXISTS {
			return ev, false
		}
		ev = audit.Event{Definition: audit.SpaceCreate, Target: spaceTarget(v.GetStorageSpace().GetRoot())}
		ev.Target.Name = r.GetName()
		if ev.Target.ID == "" {
			ev.Target.ID = r.GetName()
		}
	case *provider.UpdateStorageSpaceResponse:
		r := req.(*provider.UpdateStorageSpaceRequest)
		ev.Target = spaceTarget(r.GetStorageSpace().GetRoot())
		if ev.Target.ID == "" {
			ev.Target.ID = r.GetStorageSpace().GetId().GetOpaqueId()
		}
		// the name before the update, reported by the driver
		ev.Target.Name = item.Name
		switch {
		case utils.ExistsInOpaque(r.GetOpaque(), "restore"):
			ev.Definition = audit.SpaceEnable
		case r.GetStorageSpace().GetQuota() != nil:
			ev.Definition = audit.SpaceQuotaChange
			ev.Change = &audit.Change{After: map[string]any{"quota": r.GetStorageSpace().GetQuota().GetQuotaMaxBytes()}}
		default:
			ev.Definition = audit.SpaceUpdate
			ev.Change = spaceChange(r.GetStorageSpace())
		}
	case *provider.DeleteStorageSpaceResponse:
		r := req.(*provider.DeleteStorageSpaceRequest)
		ev = audit.Event{Definition: audit.SpaceDisable, Target: audit.Target{ID: r.GetId().GetOpaqueId(), Name: item.Name}}
		if rid, err := storagespace.ParseID(r.GetId().GetOpaqueId()); err == nil {
			ev.Target.SpaceID = storagespace.FormatStorageID(rid.GetStorageId(), rid.GetSpaceId())
		}
		if utils.ExistsInOpaque(r.GetOpaque(), "purge") {
			ev.Definition = audit.SpaceDelete
		}
	default:
		return ev, false
	}

	if s, ok := res.(su); ok {
		ev.Outcome, ev.Reason = outcome(s.GetStatus())
	}
	if ev.Target.ID == "" {
		// a failed call may not tell us more than this
		ev.Target.ID = "unknown"
	}
	return ev, true
}

// autoAccepted reports a share accepted by a service account, e.g. the
// sharing service accepting new shares automatically. Nobody decided anything,
// the share.create of the sharer is the record that counts.
func autoAccepted(ctx context.Context, res interface{}) bool {
	if _, ok := res.(*collaboration.UpdateReceivedShareResponse); !ok {
		return false
	}
	u, ok := ctxpkg.ContextGetUser(ctx)
	return ok && u.GetId().GetType() == userpb.UserType_USER_TYPE_SERVICE
}

// publicLinkAccess audits a token lookup. The share ID identifies the link;
// when the lookup failed there is none and a hash of the token is used, never
// the token itself. A denial without any credentials is the web client asking
// whether a link needs a password, not an attempt, and is left out.
func publicLinkAccess(r *link.GetPublicShareByTokenRequest, res *link.GetPublicShareByTokenResponse) (audit.Event, bool) {
	ev := audit.Event{Definition: audit.PublicLinkAccess}
	ev.Outcome, ev.Reason = outcome(res.GetStatus())

	if s := res.GetShare(); s.GetId().GetOpaqueId() != "" {
		ev.Target = shareTarget(s.GetId().GetOpaqueId(), s.GetResourceId(), "")
	} else {
		ev.Target = audit.Target{ID: "token:" + tokenHash(r.GetToken())}
	}
	ev.Target.Type = "public_link"
	ev.Actor = &audit.Actor{Type: audit.ActorPublicLink, ID: ev.Target.ID}

	if ev.Outcome == audit.OutcomeDenied {
		switch {
		case r.GetAuthentication().GetPassword() != "":
			ev.Reason = "invalid_password"
		// ocdav sends an empty signature when the request has no credentials
		case r.GetAuthentication().GetSignature().GetSignature() != "":
			ev.Reason = "invalid_signature"
		default:
			return ev, false
		}
	}
	return ev, true
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}

// accessWindow is how long repeated successful accesses of a link by the same
// client count as one access. Opening a link makes many requests, and every
// one of them looks the token up again.
const accessWindow = 30 * time.Minute

// onceWithin remembers keys for a while, to report each access only once.
type onceWithin struct {
	mu     sync.Mutex
	window time.Duration
	seen   map[string]time.Time
}

func newOnceWithin(window time.Duration) *onceWithin {
	return &onceWithin{window: window, seen: map[string]time.Time{}}
}

// first reports whether key was not seen within the window, and remembers it.
func (o *onceWithin) first(key string, now time.Time) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if t, ok := o.seen[key]; ok && now.Sub(t) < o.window {
		return false
	}
	if len(o.seen) > 10000 {
		for k, t := range o.seen {
			if now.Sub(t) >= o.window {
				delete(o.seen, k)
			}
		}
	}
	o.seen[key] = now
	return true
}

// pick returns the file or the folder definition, nil when the type is unknown.
func pick(t provider.ResourceType, file, folder *audit.Definition) *audit.Definition {
	switch t {
	case provider.ResourceType_RESOURCE_TYPE_CONTAINER:
		return folder
	case provider.ResourceType_RESOURCE_TYPE_FILE, provider.ResourceType_RESOURCE_TYPE_SYMLINK, provider.ResourceType_RESOURCE_TYPE_REFERENCE:
		return file
	default:
		return nil
	}
}

// sameParent reports whether a move only changes the name.
func sameParent(src, dst *provider.Reference) bool {
	if src.GetPath() == "" || dst.GetPath() == "" {
		return false
	}
	return storagespace.FormatResourceID(src.GetResourceId()) == storagespace.FormatResourceID(dst.GetResourceId()) &&
		path.Dir(src.GetPath()) == path.Dir(dst.GetPath())
}

// trashTarget identifies a trash bin item in the space of ref: by the ID the
// item had before it was deleted, when the driver reported it, else by its
// trash key.
func trashTarget(ref *provider.Reference, key string, item *storagespace.ItemTypeSlot) audit.Target {
	t := audit.Target{ID: key, Name: item.Name, Path: itemPath(item)}
	if rid := ref.GetResourceId(); rid != nil {
		t.SpaceID = storagespace.FormatStorageID(rid.GetStorageId(), rid.GetSpaceId())
	}
	if id := itemID(ref, item); id != "" {
		t.ID = id
	}
	return t
}

// refTarget identifies the item of ref by its ID, when the driver reported
// it. A call that failed before the item was found only has the reference.
func refTarget(ref *provider.Reference, item *storagespace.ItemTypeSlot) audit.Target {
	t := audit.Target{ID: itemID(ref, item), Path: refPath(ref, item), Name: item.Name}
	if t.ID == "" {
		t.ID, _ = storagespace.FormatReference(ref)
	}
	if rid := ref.GetResourceId(); rid != nil {
		t.SpaceID = storagespace.FormatStorageID(rid.GetStorageId(), rid.GetSpaceId())
	}
	if p := ref.GetPath(); p != "" && p != "." {
		t.Name = path.Base(p)
	}
	return t
}

// refPath is the path of the item in its space. The path of a request is
// only that when the request starts at the space root; a request through a
// share, a link or by ID gets the path from the driver.
func refPath(ref *provider.Reference, item *storagespace.ItemTypeSlot) string {
	if p := itemPath(item); p != "" {
		return p
	}
	if utils.IsSpaceRootedReference(ref) {
		return ref.GetPath()
	}
	return ""
}

// movePaths returns the paths in the space before and after a move. The
// driver knows the path of the source; a destination relative to the same
// node as the source is put under the same prefix.
func movePaths(src, dst *provider.Reference, item *storagespace.ItemTypeSlot) (before, after string) {
	before = refPath(src, item)
	switch {
	case utils.IsSpaceRootedReference(dst):
		after = dst.GetPath()
	case before != "" && utils.IsRelativeReference(dst) && utils.ResourceIDEqual(src.GetResourceId(), dst.GetResourceId()):
		rel := path.Clean(src.GetPath())
		if rel == "." {
			break
		}
		if prefix, ok := strings.CutSuffix(before, rel); ok {
			after = utils.MakeRelativePath(path.Join(prefix, dst.GetPath()))
		}
	}
	return before, after
}

// itemPath is the path in the space the driver reported, like the paths of
// requests: "./folder/file".
func itemPath(item *storagespace.ItemTypeSlot) string {
	if item.Path == "" || item.Path == "/" {
		return ""
	}
	return utils.MakeRelativePath(item.Path)
}

// itemID formats the ID the driver reported, with the storage ID of ref,
// which the driver doesn't know.
func itemID(ref *provider.Reference, item *storagespace.ItemTypeSlot) string {
	if item.ID.GetOpaqueId() == "" {
		return ""
	}
	return storagespace.FormatResourceID(&provider.ResourceId{
		StorageId: ref.GetResourceId().GetStorageId(),
		SpaceId:   item.ID.GetSpaceId(),
		OpaqueId:  item.ID.GetOpaqueId(),
	})
}

func spaceTarget(rid *provider.ResourceId) audit.Target {
	if rid == nil {
		return audit.Target{}
	}
	id := storagespace.FormatStorageID(rid.GetStorageId(), rid.GetSpaceId())
	return audit.Target{ID: id, SpaceID: id}
}

func shareTarget(id string, rid *provider.ResourceId, name string) audit.Target {
	t := audit.Target{ID: id, Name: name}
	if rid != nil {
		t.SpaceID = storagespace.FormatStorageID(rid.GetStorageId(), rid.GetSpaceId())
	}
	return t
}

func shareID(id *collaboration.ShareId, ref *collaboration.ShareReference) string {
	if id != nil {
		return id.GetOpaqueId()
	}
	return ref.GetId().GetOpaqueId()
}

// firstLinkAccess drops repeated successful accesses of a link by the same
// client, and successful ones without a client request. A failed one counts
// once per request: the proxy and ocdav both check the credentials of a
// request to public-files.
func firstLinkAccess(ctx context.Context, o *onceWithin, ev audit.Event) bool {
	if ev.Definition != audit.PublicLinkAccess {
		return true
	}
	if ev.Outcome != audit.OutcomeSuccess {
		tid := trace.SpanContextFromContext(ctx).TraceID()
		if !tid.IsValid() {
			return true
		}
		return o.first(ev.Target.ID+"|"+ev.Reason+"|"+tid.String(), time.Now())
	}
	rc, ok := audit.ContextGetRequest(ctx)
	if !ok || rc.IsZero() {
		// a service resolving the link, e.g. after an upload, is no client
		return false
	}
	return o.first(ev.Target.ID+"|"+rc.ClientAddress+"|"+rc.SessionID, time.Now())
}

// spaceAttributes are the space attributes graph puts into the opaque of an
// update, with their keys in the record. image and readme are the IDs of the
// files.
var spaceAttributes = []struct{ opaque, key string }{
	{"description", "description"},
	{"spaceAlias", "alias"},
	{"image", "image"},
	{"readme", "readme"},
}

// spaceChange lists what a space update changes. An attribute set to an
// empty value was removed.
func spaceChange(s *provider.StorageSpace) *audit.Change {
	after := map[string]any{}
	if n := s.GetName(); n != "" {
		after["name"] = n
	}
	for _, a := range spaceAttributes {
		if utils.ExistsInOpaque(s.GetOpaque(), a.opaque) {
			after[a.key] = utils.ReadPlainFromOpaque(s.GetOpaque(), a.opaque)
		}
	}
	if len(after) == 0 {
		return nil
	}
	return &audit.Change{After: after}
}
