package v1alpha1_connectiondataservice

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// sendRows streams rows, each gob-encoded the way a connection sends them.
func sendRows(t *testing.T, stream connectiondata.SampleDataStream, rows ...map[string]any) {
	t.Helper()
	for _, row := range rows {
		var buf bytes.Buffer
		require.NoError(t, gob.NewEncoder(&buf).Encode(row))
		require.NoError(t, stream.Send(&mgmtv1alpha1.GetConnectionDataStreamResponse{RowBytes: buf.Bytes()}))
	}
}

func Test_sampledValues(t *testing.T) {
	t.Run("without columns the rows are sampled and every column is kept", func(t *testing.T) {
		dataconn := connectiondata.NewMockConnectionDataService(t)
		dataconn.EXPECT().
			SampleData(mock.Anything, mock.Anything, "public", "users", uint(20)).
			RunAndReturn(func(_ context.Context, stream connectiondata.SampleDataStream, _, _ string, _ uint) error {
				sendRows(t, stream,
					map[string]any{"name": "Jane", "city": "Lyon", "note": ""},
					map[string]any{"name": "John", "city": nil},
				)
				return nil
			})

		got, err := (&Service{}).sampledValues(t.Context(), dataconn, "public", "users", nil, 20)
		require.NoError(t, err)
		require.Equal(t, map[string][]string{
			"name": {"Jane", "John"},
			"city": {"Lyon"},
		}, got)
	})

	t.Run("each named column is sampled on its own and rows are not sampled", func(t *testing.T) {
		dataconn := connectiondata.NewMockConnectionDataService(t)
		for column, values := range map[string][]any{"name": {"Jane", "John"}, "city": {"Lyon"}} {
			dataconn.EXPECT().
				SampleColumn(mock.Anything, mock.Anything, "public", "users", column, uint(20)).
				RunAndReturn(func(
					_ context.Context, stream connectiondata.SampleDataStream, _, _, _ string, _ uint,
				) error {
					for _, value := range values {
						sendRows(t, stream, map[string]any{column: value})
					}
					return nil
				}).Once()
		}

		got, err := (&Service{}).sampledValues(
			t.Context(), dataconn, "public", "users", []string{"name", "city", "name"}, 20,
		)
		require.NoError(t, err)
		require.Equal(t, map[string][]string{"name": {"Jane", "John"}, "city": {"Lyon"}}, got)
	})

	t.Run("a named column with no filled value is left out", func(t *testing.T) {
		dataconn := connectiondata.NewMockConnectionDataService(t)
		dataconn.EXPECT().
			SampleColumn(mock.Anything, mock.Anything, "public", "users", "note", uint(20)).
			Return(nil)

		got, err := (&Service{}).sampledValues(t.Context(), dataconn, "public", "users", []string{"note"}, 20)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("a connection without column sampling falls back to the rows, filtered", func(t *testing.T) {
		dataconn := connectiondata.NewMockConnectionDataService(t)
		dataconn.EXPECT().
			SampleColumn(mock.Anything, mock.Anything, "public", "users", "city", uint(20)).
			Return(errors.ErrUnsupported).Once()
		dataconn.EXPECT().
			SampleData(mock.Anything, mock.Anything, "public", "users", uint(20)).
			RunAndReturn(func(_ context.Context, stream connectiondata.SampleDataStream, _, _ string, _ uint) error {
				sendRows(t, stream, map[string]any{"name": "Jane", "city": "Lyon"})
				return nil
			}).Once()

		got, err := (&Service{}).sampledValues(
			t.Context(), dataconn, "public", "users", []string{"name", "city"}, 20,
		)
		require.NoError(t, err)
		require.Equal(t, map[string][]string{"name": {"Jane"}, "city": {"Lyon"}}, got)
	})

	t.Run("the fallback keeps only the named columns", func(t *testing.T) {
		dataconn := connectiondata.NewMockConnectionDataService(t)
		dataconn.EXPECT().
			SampleColumn(mock.Anything, mock.Anything, "public", "users", "name", uint(20)).
			Return(errors.ErrUnsupported).Once()
		dataconn.EXPECT().
			SampleData(mock.Anything, mock.Anything, "public", "users", uint(20)).
			RunAndReturn(func(_ context.Context, stream connectiondata.SampleDataStream, _, _ string, _ uint) error {
				sendRows(t, stream, map[string]any{"name": "Jane", "city": "Lyon"})
				return nil
			}).Once()

		got, err := (&Service{}).sampledValues(t.Context(), dataconn, "public", "users", []string{"name"}, 20)
		require.NoError(t, err)
		require.Equal(t, map[string][]string{"name": {"Jane"}}, got)
	})

	t.Run("a column absent from the catalogue is reported as not found", func(t *testing.T) {
		dataconn := connectiondata.NewMockConnectionDataService(t)
		dataconn.EXPECT().
			SampleColumn(mock.Anything, mock.Anything, "public", "users", "ghost", uint(20)).
			Return(connect.NewError(connect.CodeNotFound, errors.New("no such column")))

		_, err := (&Service{}).sampledValues(t.Context(), dataconn, "public", "users", []string{"ghost"}, 20)
		require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})

	t.Run("another failure is returned wrapped", func(t *testing.T) {
		boom := errors.New("boom")
		dataconn := connectiondata.NewMockConnectionDataService(t)
		dataconn.EXPECT().
			SampleColumn(mock.Anything, mock.Anything, "public", "users", "name", uint(20)).
			Return(boom)

		_, err := (&Service{}).sampledValues(t.Context(), dataconn, "public", "users", []string{"name"}, 20)
		require.ErrorIs(t, err, boom)
	})

	t.Run("a column that outlasts the deadline is named in the error", func(t *testing.T) {
		dataconn := connectiondata.NewMockConnectionDataService(t)
		dataconn.EXPECT().
			SampleColumn(mock.Anything, mock.Anything, "public", "users", "name", uint(20)).
			RunAndReturn(func(ctx context.Context, _ connectiondata.SampleDataStream, _, _, _ string, _ uint) error {
				<-ctx.Done()
				return ctx.Err()
			})

		service := &Service{sampleTimeout: 10 * time.Millisecond}
		_, err := service.sampledValues(t.Context(), dataconn, "public", "users", []string{"name"}, 20)
		require.Equal(t, connect.CodeDeadlineExceeded, connect.CodeOf(err))
		require.ErrorContains(t, err, `"name"`)
		require.ErrorContains(t, err, "public.users")
	})

	t.Run("the deadline of the row sampling names the table", func(t *testing.T) {
		dataconn := connectiondata.NewMockConnectionDataService(t)
		dataconn.EXPECT().
			SampleData(mock.Anything, mock.Anything, "public", "users", uint(20)).
			RunAndReturn(func(ctx context.Context, _ connectiondata.SampleDataStream, _, _ string, _ uint) error {
				<-ctx.Done()
				return ctx.Err()
			})

		service := &Service{sampleTimeout: 10 * time.Millisecond}
		_, err := service.sampledValues(t.Context(), dataconn, "public", "users", nil, 20)
		require.Equal(t, connect.CodeDeadlineExceeded, connect.CodeOf(err))
		require.ErrorContains(t, err, "public.users")
	})

	t.Run("each column has a deadline of its own", func(t *testing.T) {
		dataconn := connectiondata.NewMockConnectionDataService(t)
		for _, column := range []string{"a", "b"} {
			dataconn.EXPECT().
				SampleColumn(mock.Anything, mock.Anything, "public", "users", column, uint(20)).
				RunAndReturn(func(
					ctx context.Context, stream connectiondata.SampleDataStream, _, _, _ string, _ uint,
				) error {
					select {
					case <-time.After(30 * time.Millisecond):
					case <-ctx.Done():
						return ctx.Err()
					}
					sendRows(t, stream, map[string]any{column: "x"})
					return nil
				})
		}

		// Two columns of 30ms each fit in 50ms each, not in 50ms together.
		service := &Service{sampleTimeout: 50 * time.Millisecond}
		got, err := service.sampledValues(t.Context(), dataconn, "public", "users", []string{"a", "b"}, 20)
		require.NoError(t, err)
		require.Equal(t, map[string][]string{"a": {"x"}, "b": {"x"}}, got)
	})

	t.Run("a caller that gave up is returned its own error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		dataconn := connectiondata.NewMockConnectionDataService(t)
		dataconn.EXPECT().
			SampleColumn(mock.Anything, mock.Anything, "public", "users", "name", uint(20)).
			RunAndReturn(func(ctx context.Context, _ connectiondata.SampleDataStream, _, _, _ string, _ uint) error {
				return ctx.Err()
			})

		_, err := (&Service{}).sampledValues(ctx, dataconn, "public", "users", []string{"name"}, 20)
		require.ErrorIs(t, err, context.Canceled)
		require.NotEqual(t, connect.CodeDeadlineExceeded, connect.CodeOf(err))
	})
}
