package ignore

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/blobstore"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/lookup"
	"github.com/opencloud-eu/reva/v2/pkg/storage/fs/posix/options"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/tree/propagator"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/templates"
	"github.com/rs/zerolog"
)

const (
	// The needles include the separator at the beginning to avoid matching internal dir names in substrings
	// The end segment boundary is checked in isInternalDir
	trashNeedle    = needle(string(filepath.Separator) + lookup.TrashDir)
	metadataNeedle = needle(string(filepath.Separator) + lookup.MetadataDir)
	tmpNeedle      = needle(string(filepath.Separator) + blobstore.TMPDir)
)

type needle string

// Ignorer handles checking if paths should be ignored in posix operations
type Ignorer struct {
	options *options.Options
	log     *zerolog.Logger
	root    string
	// prefix is what a path in the storage starts with, the root and a separator
	prefix string
	// the space path templates as path segments of filepath.Match patterns
	templates [][]string
}

// NewIgnorer creates a new Ignorer given the posix options and logger
func NewIgnorer(options *options.Options, log *zerolog.Logger) *Ignorer {
	i := &Ignorer{
		options: options,
		log:     log,
	}
	i.root = filepath.Clean(options.Root)
	switch i.root {
	case ".":
		// filepath.Join(".", "users") is "users"
		i.prefix = ""
	case string(filepath.Separator):
		i.prefix = i.root
	default:
		i.prefix = i.root + string(filepath.Separator)
	}
	for _, tpl := range []string{options.PersonalSpacePathTemplate, options.GeneralSpacePathTemplate} {
		if tpl == "" {
			continue
		}
		i.templates = append(i.templates, strings.Split(strings.TrimPrefix(templates.Glob(tpl), "/"), "/"))
	}
	return i
}

// IsIgnored checking if paths should be ignored in posix operations
func (i *Ignorer) IsIgnored(path string) bool {
	return i.IsChanges(path) ||
		i.IsIndex(path) ||
		IsLockFile(path) ||
		i.IsTrash(path) ||
		i.IsMetadata(path) ||
		i.IsTemporary(path) ||
		i.IsUpload(path) ||
		i.IsRootPath(path) ||
		i.IsSpaceRoot(path) ||
		i.IsPurging(path)
}

func (i *Ignorer) IsChanges(path string) bool {
	return strings.HasPrefix(path, filepath.Join(i.options.Root, propagator.ChangesDir))
}

func (i *Ignorer) IsIndex(path string) bool {
	return strings.HasPrefix(path, filepath.Join(i.options.Root, lookup.IndexesDir))
}

func (i *Ignorer) IsUpload(path string) bool {
	return i.options.UploadDirectory != "" && strings.HasPrefix(path, i.options.UploadDirectory)
}

// IsRootPath reports whether path is the storage root or a directory between
// the storage root and the space roots
func (i *Ignorer) IsRootPath(path string) bool {
	if path == i.options.Root {
		return true
	}
	_, rootPath := i.classify(path)
	return rootPath
}

// IsSpaceRoot reports whether path is where the space path templates put a
// space root
func (i *Ignorer) IsSpaceRoot(path string) bool {
	spaceRoot, _ := i.classify(path)
	return spaceRoot
}

// classify matches path against the space path templates. A path below a
// space root is neither a space root nor a root path, even if a template
// matches it, e.g. users/alice/docs for {{.SpaceType}}/{{.SpaceName}}/{{.SpaceId}}.
func (i *Ignorer) classify(path string) (spaceRoot, rootPath bool) {
	if i.isStorageInternal(path) {
		return false, false
	}
	rel, ok := strings.CutPrefix(path, i.prefix)
	if !ok || rel == "" || path == i.root {
		return false, path == i.root
	}
	segments := strings.Split(rel, string(filepath.Separator))
	for n := 1; n <= len(segments); n++ {
		spaceRoot, rootPath := i.match(segments[:n])
		switch {
		case spaceRoot:
			return n == len(segments), false
		case !rootPath:
			return false, false
		case n == len(segments):
			return false, true
		}
	}
	return false, false
}

// match reports whether segments match a whole template (a space root) or
// its leading segments (a root path). If both match, the match with more
// literal segments wins, e.g. users is a root path for users/{{.User.Id.OpaqueId}}
// and not a space root for {{.SpaceId}}. A tie goes to the space root.
func (i *Ignorer) match(segments []string) (spaceRoot, rootPath bool) {
	spaceScore, rootScore := -1, -1
	for _, tpl := range i.templates {
		if len(segments) > len(tpl) {
			continue
		}
		score := 0
		for n, segment := range segments {
			if ok, _ := filepath.Match(tpl[n], segment); !ok {
				score = -1
				break
			}
			if tpl[n] != "*" {
				score++
			}
		}
		if len(segments) == len(tpl) {
			spaceScore = max(spaceScore, score)
		} else {
			rootScore = max(rootScore, score)
		}
	}
	if spaceScore >= 0 && spaceScore >= rootScore {
		return true, false
	}
	return false, rootScore >= 0
}

// SpaceRoots returns the paths in the storage that are space roots according
// to the space path templates
func (i *Ignorer) SpaceRoots() ([]string, error) {
	var roots []string
	seen := map[string]bool{}
	for _, tpl := range i.templates {
		dirs := []string{i.root}
		// a directory that only a wildcard matched can be unrelated to the
		// storage, e.g. a lost+found in the storage root
		wildcard := false
		for _, pattern := range tpl {
			var next []string
			for _, dir := range dirs {
				entries, err := os.ReadDir(dir)
				// a pattern can match a file, e.g. a .DS_Store in the storage root
				if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
					continue
				}
				if err != nil && wildcard {
					i.log.Warn().Err(err).Str("path", dir).Msg("could not read a directory while looking for space roots, skipping it")
					continue
				}
				if err != nil {
					return nil, err
				}
				for _, entry := range entries {
					path := filepath.Join(dir, entry.Name())
					if ok, _ := filepath.Match(pattern, entry.Name()); ok && !i.isStorageInternal(path) {
						next = append(next, path)
					}
				}
			}
			dirs = next
			wildcard = wildcard || pattern == "*"
		}
		for _, dir := range dirs {
			if !seen[dir] && i.IsSpaceRoot(dir) {
				seen[dir] = true
				roots = append(roots, dir)
			}
		}
	}
	return roots, nil
}

// the path templates can match these directories, but they never hold spaces
func (i *Ignorer) isStorageInternal(path string) bool {
	return i.IsChanges(path) || i.IsIndex(path) || i.IsUpload(path)
}

// IsPurging checks if the path lies in a space root that was renamed for purging
func (i *Ignorer) IsPurging(path string) bool {
	for _, root := range []string{i.personalSpacesRoot, i.projectSpacesRoot} {
		if rel, ok := strings.CutPrefix(path, root+string(filepath.Separator)); ok && strings.HasPrefix(rel, lookup.PurgeDirPrefix) {
			return true
		}
	}
	return false
}

func IsLockFile(path string) bool {
	return strings.HasSuffix(path, ".flock") || strings.HasSuffix(path, ".mlock")
}

func (i *Ignorer) IsMetadata(path string) bool {
	return i.isInternalDir(path, metadataNeedle)
}

func (i *Ignorer) IsTemporary(path string) bool {
	return i.isInternalDir(path, tmpNeedle)
}

func (i *Ignorer) IsTrash(path string) bool {
	return i.isInternalDir(path, trashNeedle)
}

// isInternalDir checks if the path contains the match dir and that the match lives
// in the space root, e.g. "/storage/users/user1/.metadata/file" -> match is ".metadata",
// parent dir is "/storage/users/user1" which is a space root, so this would return true
func (i *Ignorer) isInternalDir(path string, match needle) bool {
	idx := strings.Index(path, string(match))
	if idx <= 0 {
		return false
	}

	// must end at a segment boundary (end of path or separator)
	if length := idx + len(match); length != len(path) && path[length] != filepath.Separator {
		return false
	}

	// get the path of the parent dir, e.g. "/a/.Trash" -> the needle "/.Trash"
	// starts at index 2, so parentPath is path[:2] -> "/a"
	parentPath := path[:idx]

	return len(parentPath) > 0 && i.IsSpaceRoot(parentPath)
}
