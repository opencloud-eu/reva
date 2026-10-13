// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package appprovider

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	appproviderpb "github.com/cs3org/go-cs3apis/cs3/app/provider/v1beta1"
	appregistry "github.com/cs3org/go-cs3apis/cs3/app/registry/v1beta1"
	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/status"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type gatewayServer struct {
	gateway.UnimplementedGatewayAPIServer

	providers []*appregistry.ProviderInfo

	mu        sync.Mutex
	openedApp string
}

func (g *gatewayServer) Stat(ctx context.Context, _ *provider.StatRequest) (*provider.StatResponse, error) {
	return &provider.StatResponse{
		Status: status.NewOK(ctx),
		Info: &provider.ResourceInfo{
			Type: provider.ResourceType_RESOURCE_TYPE_FILE,
			Path: "/report.odt",
			PermissionSet: &provider.ResourcePermissions{
				InitiateFileDownload: true,
				InitiateFileUpload:   true,
			},
		},
	}, nil
}

func (g *gatewayServer) GetAppProviders(ctx context.Context, _ *appregistry.GetAppProvidersRequest) (*appregistry.GetAppProvidersResponse, error) {
	return &appregistry.GetAppProvidersResponse{Status: status.NewOK(ctx), Providers: g.providers}, nil
}

func (g *gatewayServer) OpenInApp(ctx context.Context, req *gateway.OpenInAppRequest) (*appproviderpb.OpenInAppResponse, error) {
	g.mu.Lock()
	g.openedApp = req.GetApp()
	g.mu.Unlock()

	return &appproviderpb.OpenInAppResponse{
		Status: status.NewOK(ctx),
		AppUrl: &appproviderpb.OpenInAppURL{AppUrl: "https://app.example.com/edit"},
	}, nil
}

func startGateway(t *testing.T, gw *gatewayServer) string {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := grpc.NewServer()
	gateway.RegisterGatewayAPIServer(srv, gw)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	return "dns:///" + lis.Addr().String()
}

func TestOpenWithWebAppFallback(t *testing.T) {
	cases := []struct {
		name       string
		appName    string
		providers  []*appregistry.ProviderInfo
		wantStatus int
		wantApp    string
		wantCode   appErrorCode
	}{
		{
			name:       "requested app is used",
			appName:    "OnlyOffice",
			providers:  []*appregistry.ProviderInfo{{Name: "Collabora"}},
			wantStatus: http.StatusOK,
			wantApp:    "OnlyOffice",
		},
		{
			name:       "no app falls back to the first provider",
			providers:  []*appregistry.ProviderInfo{{Name: "Collabora"}, {Name: "OnlyOffice"}},
			wantStatus: http.StatusOK,
			wantApp:    "Collabora",
		},
		{
			name:       "no app and no provider is not found",
			wantStatus: http.StatusNotFound,
			wantCode:   appErrorProviderNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := &gatewayServer{providers: tc.providers}
			logger := zerolog.Nop()
			s, err := New(map[string]interface{}{
				"gatewaysvc": startGateway(t, gw),
				"webbaseuri": "https://cloud.example.com/external",
				"web": map[string]interface{}{
					"urlparamsmapping": map[string]string{"fileId": "fileid", "app": "appname"},
				},
			}, &logger)
			require.NoError(t, err)

			form := url.Values{"file_id": {"storage$space!node"}}
			if tc.appName != "" {
				form.Set("app_name", tc.appName)
			}
			req := httptest.NewRequest(http.MethodPost, "/open-with-web?"+form.Encode(), nil)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)

			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())

			if tc.wantCode != "" {
				var appErr appError
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &appErr))
				require.Equal(t, tc.wantCode, appErr.Code)
				return
			}

			gw.mu.Lock()
			require.Equal(t, tc.wantApp, gw.openedApp)
			gw.mu.Unlock()

			require.Contains(t, rec.Body.String(), "&")
			require.NotContains(t, rec.Body.String(), "\\u0026")
			var res openInWebResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
			u, err := url.Parse(res.URI)
			require.NoError(t, err)
			require.Equal(t, tc.wantApp, u.Query().Get("app"))
		})
	}
}
