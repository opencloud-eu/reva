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

type moveStorage struct {
	storage.FS
	result *storage.MoveResult
	err    error
}

func (s moveStorage) Move(context.Context, *provider.Reference, *provider.Reference) (*storage.MoveResult, error) {
	return s.result, s.err
}

func TestMoveResultPropagation(t *testing.T) {
	result := &storage.MoveResult{
		OldReference: &provider.Reference{Path: "/old"},
		NewReference: &provider.Reference{Path: "/new"},
	}
	for _, tt := range []struct {
		name   string
		result *storage.MoveResult
		err    error
		code   rpc.Code
		want   *storage.MoveResult
	}{
		{name: "resolved move", result: result, code: rpc.Code_CODE_OK, want: result},
		{name: "driver without metadata", code: rpc.Code_CODE_OK},
		{name: "failed move", result: result, err: errtypes.NotFound("source"), code: rpc.Code_CODE_NOT_FOUND},
		{name: "internal error", err: errors.New("move failed"), code: rpc.Code_CODE_INTERNAL},
	} {
		t.Run(tt.name, func(t *testing.T) {
			slot := &storagespace.MoveResultSlot{}
			ctx := storagespace.ContextRegisterMoveResultSlot(context.Background(), slot)
			s := &Service{Storage: moveStorage{result: tt.result, err: tt.err}}

			response, err := s.Move(ctx, &provider.MoveRequest{})

			require.NoError(t, err)
			require.Equal(t, tt.code, response.Status.Code)
			require.Same(t, tt.want, slot.Result)
		})
	}
}
