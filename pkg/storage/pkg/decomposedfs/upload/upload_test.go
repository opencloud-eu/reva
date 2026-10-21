package upload_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tusd "github.com/tus/tusd/v2/pkg/handler"

	"github.com/opencloud-eu/reva/v2/pkg/errtypes"

	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/aspects"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/options"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/upload"
)

func TestServeContent(t *testing.T) {
	log := &zerolog.Logger{}
	root := t.TempDir()
	store := upload.NewSessionStore(nil, aspects.Aspects{}, root, false, options.TokenOptions{}, log)
	session := store.New(context.Background())

	root = filepath.Join(root, "uploads")
	assert.NoError(t, os.MkdirAll(root, 0755))

	tmpFile, err := os.Create(filepath.Join(root, session.ID()))
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, tmpFile.Close())
	}()

	_, err = tmpFile.WriteString("Hello, World!")
	assert.NoError(t, err)

	req, err := http.NewRequest("GET", "/", nil)
	assert.NoError(t, err)

	t.Run("contains the whole file without a range header", func(t *testing.T) {
		rr := httptest.NewRecorder()

		assert.NoError(t, session.ServeContent(context.Background(), rr, req))
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Empty(t, rr.Header().Get("Content-Range"))

		body, err := io.ReadAll(rr.Body)
		assert.NoError(t, err)
		assert.Equal(t, "Hello, World!", string(body))
	})

	t.Run("contains the whole file with a range header even if the range is invalid", func(t *testing.T) {
		req.Header.Set("Range", "bytes=0-100")
		rr := httptest.NewRecorder()

		assert.NoError(t, session.ServeContent(context.Background(), rr, req))
		assert.Equal(t, http.StatusPartialContent, rr.Code)
		assert.Equal(t, "bytes 0-12/13", rr.Header().Get("Content-Range"))

		body, err := io.ReadAll(rr.Body)
		assert.NoError(t, err)
		assert.Equal(t, "Hello, World!", string(body))
	})

	t.Run("contains bytes 0-4", func(t *testing.T) {
		req.Header.Set("Range", "bytes=0-4")
		rr := httptest.NewRecorder()

		assert.NoError(t, session.ServeContent(context.Background(), rr, req))
		assert.Equal(t, http.StatusPartialContent, rr.Code)
		assert.Equal(t, "bytes 0-4/13", rr.Header().Get("Content-Range"))

		body, err := io.ReadAll(rr.Body)
		assert.NoError(t, err)
		assert.Equal(t, "Hello", string(body))
	})

	t.Run("contains bytes 4-4", func(t *testing.T) {
		req.Header.Set("Range", "bytes=4-4")
		rr := httptest.NewRecorder()

		assert.NoError(t, session.ServeContent(context.Background(), rr, req))
		assert.Equal(t, http.StatusPartialContent, rr.Code)
		assert.Equal(t, "bytes 4-4/13", rr.Header().Get("Content-Range"))

		body, err := io.ReadAll(rr.Body)
		assert.NoError(t, err)
		assert.Equal(t, "o", string(body))
	})
}

func newTestSession(t *testing.T, size int64) (*upload.DecomposedFsSession, string) {
	t.Helper()
	log := &zerolog.Logger{}
	root := t.TempDir()
	store := upload.NewSessionStore(nil, aspects.Aspects{}, root, false, options.TokenOptions{}, log)
	session := store.New(context.Background())
	session.SetSize(size)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "uploads"), 0755))
	require.NoError(t, session.TouchBin())
	return session, filepath.Join(root, "uploads", session.ID())
}

func TestWriteChunkRejectsStaleOffset(t *testing.T) {
	ctx := context.Background()
	session, binPath := newTestSession(t, 10)

	n, err := session.WriteChunk(ctx, 0, strings.NewReader("01234"))
	require.NoError(t, err)
	assert.Equal(t, int64(5), n)

	// a second request that still believes the upload is at offset 0 must not append
	n, err = session.WriteChunk(ctx, 0, strings.NewReader("01234"))
	var tusErr tusd.Error
	require.True(t, errors.As(err, &tusErr), "expected a tusd error, got %v", err)
	assert.Equal(t, http.StatusConflict, tusErr.HTTPResponse.StatusCode)
	assert.Equal(t, int64(0), n)

	data, err := os.ReadFile(binPath)
	require.NoError(t, err)
	assert.Equal(t, "01234", string(data))

	// resuming from the actual offset still works
	n, err = session.WriteChunk(ctx, 5, strings.NewReader("56789"))
	require.NoError(t, err)
	assert.Equal(t, int64(5), n)
	data, err = os.ReadFile(binPath)
	require.NoError(t, err)
	assert.Equal(t, "0123456789", string(data))
}

func TestFinishUploadRejectsSizeMismatch(t *testing.T) {
	ctx := context.Background()
	session, binPath := newTestSession(t, 10)
	require.NoError(t, os.WriteFile(binPath, []byte("01234"), 0600))

	err := session.FinishUploadDecomposed(ctx)
	require.Error(t, err)
	assert.IsType(t, errtypes.ChecksumMismatch(""), err)
	assert.Contains(t, err.Error(), "expected 10 bytes, got 5")
	assert.NoFileExists(t, binPath, "the staging file of a rejected upload must be removed")
}
