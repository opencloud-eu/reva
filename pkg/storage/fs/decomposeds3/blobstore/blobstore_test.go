// SPDX-License-Identifier: Apache-2.0

package blobstore

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/opencloud-eu/reva/v2/pkg/storage/pkg/decomposedfs/node"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSSECHeaders(t *testing.T) {
	source := filepath.Join(t.TempDir(), "blob")
	if err := os.WriteFile(source, []byte("blob"), 0600); err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32))
	for _, tc := range []struct{ name, key string }{{"unset", ""}, {"SSE-C", key}} {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{DisableMultipart: true, SendContentMd5: true}
			if tc.key != "" {
				if _, err := New("http://s3.example.com", "us-east-1", "bucket", "ak", "sk", tc.key, opts); err == nil {
					t.Fatal("accepted an HTTP endpoint with SSE-C enabled")
				}
			}
			bs, err := New("https://s3.example.com", "us-east-1", "bucket", "ak", "sk", tc.key, opts)
			if err != nil {
				t.Fatal(err)
			}
			methods := map[string]int{}
			// Intercept SDK requests to check Reva's option wiring without a
			// network server or emulating multipart behavior owned by the SDK.
			bs.client, err = minio.New("s3.example.com", &minio.Options{
				Region: "us-east-1", Secure: true,
				Creds: credentials.NewStaticV4("ak", "sk", ""),
				Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					methods[r.Method]++
					if r.Header.Get("X-Amz-Server-Side-Encryption-Customer-Key") != tc.key {
						t.Errorf("%s request has the wrong customer key", r.Method)
					}
					algorithm := ""
					if tc.key != "" {
						algorithm = "AES256"
					}
					if r.Header.Get("X-Amz-Server-Side-Encryption-Customer-Algorithm") != algorithm {
						t.Errorf("%s request has the wrong encryption algorithm", r.Method)
					}
					if r.Body != nil {
						defer r.Body.Close()
						if _, err := io.Copy(io.Discard, r.Body); err != nil {
							return nil, err
						}
					}
					headers := http.Header{}
					headers.Set("Content-Length", "4")
					headers.Set("ETag", `"etag"`)
					headers.Set("Last-Modified", "Mon, 26 Oct 2026 12:00:00 GMT")
					return &http.Response{StatusCode: http.StatusOK, Header: headers,
						ContentLength: 4, Body: io.NopCloser(strings.NewReader("blob")), Request: r}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			n := &node.Node{BaseNode: node.BaseNode{SpaceID: "space"}, BlobID: "blob", Blobsize: 4}
			if err := bs.Upload(n, source, ""); err != nil {
				t.Fatalf("Upload: %v", err)
			}
			if err := bs.UploadFromReader(n, strings.NewReader("blob"), 4); err != nil {
				t.Fatalf("UploadFromReader: %v", err)
			}
			reader, err := bs.Download(n)
			if err != nil {
				t.Fatalf("Download: %v", err)
			}
			defer reader.Close()
			if _, err := io.Copy(io.Discard, reader); err != nil {
				t.Fatal(err)
			}
			if methods[http.MethodPut] != 2 || methods[http.MethodHead] != 1 || methods[http.MethodGet] != 1 {
				t.Fatalf("unexpected requests: %v", methods)
			}
		})
	}
}
