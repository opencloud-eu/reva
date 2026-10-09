// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package upload_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tusd "github.com/tus/tusd/v2/pkg/handler"

	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/aspects"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/options"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/upload"
)

func TestFinishUploadStatusCodes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		checksum string
		status   int
	}{
		{"checksum mismatch", "sha1 0000000000000000000000000000000000000000", 460},
		{"invalid checksum format", "sha1", http.StatusBadRequest},
		{"unsupported checksum algorithm", "crc32 00000000", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store := upload.NewSessionStore(nil, aspects.Aspects{}, root, false, options.TokenOptions{}, &zerolog.Logger{})
			session := store.New(context.Background())
			session.SetSize(5)
			session.SetMetadata("checksum", tc.checksum)
			require.NoError(t, os.MkdirAll(filepath.Join(root, "uploads"), 0755))
			require.NoError(t, os.WriteFile(filepath.Join(root, "uploads", session.ID()), []byte("01234"), 0600))

			err := session.FinishUpload(context.Background())

			var tusErr tusd.Error
			require.True(t, errors.As(err, &tusErr), "expected a tusd error, got %v", err)
			assert.Equal(t, tc.status, tusErr.HTTPResponse.StatusCode)
		})
	}
}
