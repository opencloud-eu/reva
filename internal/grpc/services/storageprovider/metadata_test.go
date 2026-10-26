package storageprovider

import (
	"context"
	"errors"
	"testing"

	rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/errtypes"
	"github.com/opencloud-eu/reva/v2/pkg/storage"
	"github.com/opencloud-eu/reva/v2/pkg/storagespace"
	"github.com/stretchr/testify/require"
)

type metadataStorage struct {
	storage.FS
	result *storage.MetadataResult
	err    error
}

func (s metadataStorage) SetArbitraryMetadata(context.Context, *provider.Reference, *provider.ArbitraryMetadata) (*storage.MetadataResult, error) {
	return s.result, s.err
}

func (s metadataStorage) UnsetArbitraryMetadata(context.Context, *provider.Reference, []string) (*storage.MetadataResult, error) {
	return s.result, s.err
}

func TestMetadataResultPropagation(t *testing.T) {
	result := &storage.MetadataResult{ChangedKeys: []string{"foo"}, RemovedKeys: []string{"bar"}}
	for _, tt := range []struct {
		name   string
		result *storage.MetadataResult
		err    error
		code   rpc.Code
		want   *storage.MetadataResult
	}{
		{name: "reported keys", result: result, code: rpc.Code_CODE_OK, want: result},
		{name: "driver without result", code: rpc.Code_CODE_OK},
		{name: "failed call", result: result, err: errtypes.NotFound("resource"), code: rpc.Code_CODE_NOT_FOUND},
		{name: "internal error", err: errors.New("write failed"), code: rpc.Code_CODE_INTERNAL},
	} {
		t.Run(tt.name+" on set", func(t *testing.T) {
			slot := &storagespace.MetadataResultSlot{}
			ctx := storagespace.ContextRegisterMetadataResultSlot(context.Background(), slot)
			s := &Service{Storage: metadataStorage{result: tt.result, err: tt.err}}

			response, err := s.SetArbitraryMetadata(ctx, &provider.SetArbitraryMetadataRequest{})

			require.NoError(t, err)
			require.Equal(t, tt.code, response.Status.Code)
			require.Same(t, tt.want, slot.Result)
		})
		t.Run(tt.name+" on unset", func(t *testing.T) {
			slot := &storagespace.MetadataResultSlot{}
			ctx := storagespace.ContextRegisterMetadataResultSlot(context.Background(), slot)
			s := &Service{Storage: metadataStorage{result: tt.result, err: tt.err}}

			response, err := s.UnsetArbitraryMetadata(ctx, &provider.UnsetArbitraryMetadataRequest{})

			require.NoError(t, err)
			require.Equal(t, tt.code, response.Status.Code)
			require.Same(t, tt.want, slot.Result)
		})
	}
}
