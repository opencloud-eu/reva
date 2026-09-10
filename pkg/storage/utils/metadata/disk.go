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

package metadata

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	typesv1beta1 "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/google/renameio/v2"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage/utils/metadata/locks"
)

// contentEtag returns an etag derived from the file's content (md5), so that it
// changes whenever the bytes change — not merely when mtime or size do. This is
// what makes the etag a reliable cache-invalidation token for read-modify-write
// cycles, including same-size writes that happen within the same second.
func contentEtag(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// Disk represents a disk metadata storage
type Disk struct {
	dataDir string
	locker  locks.Locker
}

// NewDiskStorage returns a new disk storage instance
func NewDiskStorage(dataDir string, opts ...Option) (s Storage, err error) {
	c := applyOptions(opts)
	if _, ok := c.locker.(*locks.DiskLocker); !ok {
		// When no explicit locker was supplied, root the default disk locker at
		// the data dir so sidecar lock files are written next to the data.
		c.locker = locks.NewDiskLocker(dataDir)
	}
	return &Disk{
		dataDir: dataDir,
		locker:  c.locker,
	}, nil
}

// Init creates the metadata space
func (disk *Disk) Init(_ context.Context, _ string) (err error) {
	return os.MkdirAll(disk.dataDir, 0777)
}

// Backend returns the backend name of the storage
func (disk *Disk) Backend() string {
	return "disk"
}

// Stat returns the metadata for the given path
func (disk *Disk) Stat(ctx context.Context, path string) (*provider.ResourceInfo, error) {
	info, err := os.Stat(disk.targetPath(path))
	if err != nil {
		var pathError *fs.PathError
		if errors.As(err, &pathError) {
			return nil, errtypes.NotFound("path not found: " + path)
		}
		return nil, err
	}
	entry := &provider.ResourceInfo{
		Type:  provider.ResourceType_RESOURCE_TYPE_FILE,
		Path:  "./" + info.Name(),
		Name:  info.Name(),
		Mtime: &typesv1beta1.Timestamp{Seconds: uint64(info.ModTime().Unix()), Nanos: uint32(info.ModTime().Nanosecond())},
	}
	if info.IsDir() {
		entry.Type = provider.ResourceType_RESOURCE_TYPE_CONTAINER
		return entry, nil
	}
	entry.Etag, err = contentEtag(disk.targetPath(info.Name()))
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// SimpleUpload stores a file on disk
func (disk *Disk) SimpleUpload(ctx context.Context, uploadpath string, content []byte) error {
	_, err := disk.Upload(ctx, UploadRequest{
		Path:    uploadpath,
		Content: content,
	})
	return err
}

// Upload stores a file on disk
func (disk *Disk) Upload(_ context.Context, req UploadRequest) (*UploadResponse, error) {
	p := disk.targetPath(req.Path)

	// IfNoneMatch: ["*"] means create the file only if it does not already
	// exist. Use O_EXCL so the check and the create are atomic on the local
	// filesystem.
	for _, tag := range req.IfNoneMatch {
		if tag != "*" {
			continue
		}
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return nil, errtypes.AlreadyExists(p)
			}
			return nil, err
		}
		if _, err := f.Write(req.Content); err != nil {
			_ = f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
		res := &UploadResponse{}
		res.Etag, err = contentEtag(p)
		if err != nil {
			return nil, err
		}
		return res, nil
	}

	if req.IfMatchEtag != "" {
		if _, err := os.Stat(p); err == nil {
			// File exists: verify its content matches the expected etag.
			etag, err := contentEtag(p)
			if err != nil {
				return nil, err
			}
			if etag != req.IfMatchEtag {
				return nil, errtypes.PreconditionFailed("etag mismatch")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if req.IfUnmodifiedSince != (time.Time{}) {
		info, err := os.Stat(p)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		} else if err == nil {
			if info.ModTime().After(req.IfUnmodifiedSince) {
				return nil, errtypes.PreconditionFailed(fmt.Sprintf("resource has been modified, mtime: %s > since %s", info.ModTime(), req.IfUnmodifiedSince))
			}
		}
	}
	err := os.WriteFile(p, req.Content, 0644)
	if err != nil {
		return nil, err
	}

	res := &UploadResponse{}
	res.Etag, err = contentEtag(p)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// UploadWithLock performs an atomic read-modify-write on req.Path.
//
// The exclusive lock for the path is held across the entire
// read → mutate → write cycle, which is what makes the operation atomic: no
// other writer can interleave between reading the current content and writing
// the new one. fn receives the current content (nil if the file does not exist)
// and returns the bytes to persist; returning nil from fn skips the write.
func (disk *Disk) UploadWithLock(ctx context.Context, req UploadRequest, fn func(existing []byte) ([]byte, error)) (*UploadResponse, error) {
	// The lock key is the logical path; the locker resolves it against its own
	// base directory so the sidecar lock file lands next to the data file.
	unlock, err := disk.locker.Lock(ctx, req.Path)
	if err != nil {
		return nil, err
	}
	defer unlock()

	p := disk.targetPath(req.Path)

	// Read the current content. A missing file is not an error: it simply means
	// fn will receive a nil existing value (the create path).
	existing, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			existing = nil
		} else {
			return nil, err
		}
	}

	// Let the caller mutate the content. A nil result means "do not write".
	newContent, err := fn(existing)
	if err != nil {
		return nil, err
	}
	if newContent == nil {
		etag, _ := disk.currentEtag(p)
		return &UploadResponse{Etag: etag}, nil
	}

	// IfNoneMatch: ["*"] means create the file only if it does not already exist.
	for _, tag := range req.IfNoneMatch {
		if tag == "*" && existing != nil {
			return nil, errtypes.AlreadyExists(req.Path)
		}
	}

	// Write atomically so a reader never observes a partially written file.
	if err := renameio.WriteFile(p, newContent, 0644); err != nil {
		return nil, err
	}

	res := &UploadResponse{}
	res.Etag = fmt.Sprintf("%x", md5.Sum(newContent))
	return res, nil
}

// currentEtag returns the etag for an existing file, or "" if it does not exist.
func (disk *Disk) currentEtag(p string) (string, error) {
	if _, err := os.Stat(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return contentEtag(p)
}

// Download reads a file from disk
func (disk *Disk) Download(_ context.Context, req DownloadRequest) (*DownloadResponse, error) {
	var err error

	f, err := os.Open(disk.targetPath(req.Path))
	if err != nil {
		var pathError *fs.PathError
		if errors.As(err, &pathError) {
			return nil, errtypes.NotFound("path not found: " + disk.targetPath(req.Path))
		}
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	res := DownloadResponse{}
	res.Mtime = info.ModTime()

	res.Content, err = io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	res.Etag = fmt.Sprintf("%x", md5.Sum(res.Content))
	return &res, nil
}

// SimpleDownload reads a file from disk
func (disk *Disk) SimpleDownload(ctx context.Context, downloadpath string) ([]byte, error) {
	res, err := disk.Download(ctx, DownloadRequest{Path: downloadpath})
	if err != nil {
		return nil, err
	}
	return res.Content, nil
}

// Delete deletes a path
func (disk *Disk) Delete(_ context.Context, path string) error {
	return os.Remove(disk.targetPath(path))
}

// ReadDir returns the resource infos in a given directory
func (disk *Disk) ReadDir(_ context.Context, p string) ([]string, error) {
	infos, err := os.ReadDir(disk.targetPath(p))
	if err != nil {
		if _, ok := err.(*fs.PathError); ok {
			return []string{}, nil
		}
		return nil, err
	}

	entries := make([]string, 0, len(infos))
	for _, entry := range infos {
		entries = append(entries, filepath.Join(p, entry.Name()))
	}
	return entries, nil
}

// ListDir returns a list of ResourceInfos for the entries in a given directory
func (disk *Disk) ListDir(ctx context.Context, path string) ([]*provider.ResourceInfo, error) {
	diskEntries, err := os.ReadDir(disk.targetPath(path))
	if err != nil {
		if _, ok := err.(*fs.PathError); ok {
			return []*provider.ResourceInfo{}, nil
		}
		return nil, err
	}

	entries := make([]*provider.ResourceInfo, 0, len(diskEntries))
	for _, diskEntry := range diskEntries {
		info, err := diskEntry.Info()
		if err != nil {
			continue
		}

		entry := &provider.ResourceInfo{
			Type:  provider.ResourceType_RESOURCE_TYPE_FILE,
			Path:  "./" + info.Name(),
			Name:  info.Name(),
			Mtime: &typesv1beta1.Timestamp{Seconds: uint64(info.ModTime().Unix()), Nanos: uint32(info.ModTime().Nanosecond())},
		}
		if info.IsDir() {
			entry.Type = provider.ResourceType_RESOURCE_TYPE_CONTAINER
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// MakeDirIfNotExist will create a root node in the metadata storage. Requires an authenticated context.
func (disk *Disk) MakeDirIfNotExist(_ context.Context, path string) error {
	return os.MkdirAll(disk.targetPath(path), 0777)
}

// CreateSymlink creates a symlink
func (disk *Disk) CreateSymlink(_ context.Context, oldname, newname string) error {
	return os.Symlink(oldname, disk.targetPath(newname))
}

// ResolveSymlink resolves a symlink
func (disk *Disk) ResolveSymlink(_ context.Context, path string) (string, error) {
	return os.Readlink(disk.targetPath(path))
}

func (disk *Disk) targetPath(p string) string {
	return filepath.Join(disk.dataDir, filepath.Join("/", p))
}
