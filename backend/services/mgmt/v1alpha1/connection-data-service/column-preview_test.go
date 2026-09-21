package v1alpha1_connectiondataservice

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	jsonanonymizer "github.com/fishtre-compagnie/husonym/internal/json-anonymizer"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_previewValues(t *testing.T) {
	t.Run("a constant shows as fewer distinct values after than before", func(t *testing.T) {
		// The signal the preview exists to give: three logins become one, which a unique index
		// would refuse on the second row. The run would only have said so after minutes.
		resp := previewValues(
			[]any{"alice", "bruno", "chloe"},
			func(any) (any, error) { return "demo", nil },
		)
		require.Equal(t, uint32(3), resp.GetDistinctInputs())
		require.Equal(t, uint32(1), resp.GetDistinctOutputs())
		require.Len(t, resp.GetValues(), 3)
		require.Equal(t, "demo", resp.GetValues()[0].GetOutput().GetValue())
	})

	t.Run("a failure is reported against its value and does not stop the others", func(t *testing.T) {
		resp := previewValues(
			[]any{"ok", "boom", "ok too"},
			func(v any) (any, error) {
				if v == "boom" {
					return nil, errors.New("cannot transform")
				}
				return v, nil
			},
		)
		require.Len(t, resp.GetValues(), 3)
		require.Equal(t, "cannot transform", resp.GetValues()[1].GetError())
		require.Nil(t, resp.GetValues()[1].GetOutput())
		require.Equal(t, "ok too", resp.GetValues()[2].GetOutput().GetValue())
		// The failed value produced nothing, so it counts on one side only.
		require.Equal(t, uint32(3), resp.GetDistinctInputs())
		require.Equal(t, uint32(2), resp.GetDistinctOutputs())
	})

	t.Run("nulls are shown and left out of the counts", func(t *testing.T) {
		resp := previewValues(
			[]any{nil, "x"},
			func(v any) (any, error) { return v, nil },
		)
		require.True(t, resp.GetValues()[0].GetInput().GetIsNull())
		require.True(t, resp.GetValues()[0].GetOutput().GetIsNull())
		require.Equal(t, uint32(1), resp.GetDistinctInputs())
		require.Equal(t, uint32(1), resp.GetDistinctOutputs())
	})
}

func Test_transformValue(t *testing.T) {
	// Through the real anonymizer, so the wrapping — {"value": ...} and the .value expression —
	// is what is under test, not a stand-in for it.
	anonymizer, err := jsonanonymizer.NewAnonymizer(
		jsonanonymizer.WithTransformerMappings([]*mgmtv1alpha1.TransformerMapping{{
			Expression: ".value",
			Transformer: &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
					TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{
						Code: `return value.toUpperCase();`,
					},
				},
			},
		}}),
	)
	require.NoError(t, err)

	out, err := transformValue(anonymizer, "alice")
	require.NoError(t, err)
	require.Equal(t, "ALICE", out)
}

// An age written as a JSON number — "age":28 — and not as a string — "age":"28".
var unquotedNumber = regexp.MustCompile(`"age":[0-9]+`)

func Test_previewJavascript(t *testing.T) {
	sampled := &sampledTable{
		accountId: "22222222-2222-2222-2222-222222222222",
		rows: []map[string]any{
			{"age": int64(28), "name": "Alice"},
			{"age": int64(31), "name": "Bruno"},
		},
	}
	config := &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
			TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: `return value + 1;`},
		},
	}

	t.Run("hands the rule the whole row with its numbers as numbers", func(t *testing.T) {
		client := mgmtv1alpha1connect.NewMockTransformersServiceClient(t)
		client.EXPECT().
			TryJavascriptRules(mock.Anything, mock.MatchedBy(
				func(req *connect.Request[mgmtv1alpha1.TryJavascriptRulesRequest]) bool {
					// A number, not "28": this is the whole reason for this path. And the other
					// columns ride along, since a rule may read them through `input`.
					return len(req.Msg.GetRows()) == 1 &&
						unquotedNumber.MatchString(req.Msg.GetRows()[0]) &&
						strings.Contains(req.Msg.GetRows()[0], `"name":`) &&
						req.Msg.GetAccountId() == sampled.accountId
				},
			)).
			RunAndReturn(func(
				_ context.Context,
				req *connect.Request[mgmtv1alpha1.TryJavascriptRulesRequest],
			) (*connect.Response[mgmtv1alpha1.TryJavascriptRulesResponse], error) {
				out := strings.Replace(req.Msg.GetRows()[0], `"age":28`, `"age":29`, 1)
				out = strings.Replace(out, `"age":31`, `"age":32`, 1)
				return connect.NewResponse(&mgmtv1alpha1.TryJavascriptRulesResponse{Rows: []string{out}}), nil
			})

		s := &Service{transformers: Transformers{Client: client}}
		resp := s.previewJavascript(context.Background(), sampled, "age", []any{int64(28), int64(31)}, config)

		require.Len(t, resp.GetValues(), 2)
		// 28 + 1 = 29, as the run computes it — not "281".
		require.Equal(t, "29", resp.GetValues()[0].GetOutput().GetValue())
		require.Equal(t, "32", resp.GetValues()[1].GetOutput().GetValue())
	})

	t.Run("a failing row keeps its message and the others still run", func(t *testing.T) {
		client := mgmtv1alpha1connect.NewMockTransformersServiceClient(t)
		client.EXPECT().
			TryJavascriptRules(mock.Anything, mock.Anything).
			RunAndReturn(func(
				_ context.Context,
				req *connect.Request[mgmtv1alpha1.TryJavascriptRulesRequest],
			) (*connect.Response[mgmtv1alpha1.TryJavascriptRulesResponse], error) {
				if strings.Contains(req.Msg.GetRows()[0], "Alice") {
					return connect.NewResponse(&mgmtv1alpha1.TryJavascriptRulesResponse{
						Failure: &mgmtv1alpha1.JavascriptRuleFailure{Message: "TypeError: boom"},
					}), nil
				}
				return connect.NewResponse(&mgmtv1alpha1.TryJavascriptRulesResponse{
					Rows: []string{`{"age":32,"name":"Bruno"}`},
				}), nil
			})

		s := &Service{transformers: Transformers{Client: client}}
		resp := s.previewJavascript(context.Background(), sampled, "age", []any{int64(28), int64(31)}, config)

		// The message the trial carries, unlike the anonymizer's, which comes out empty.
		require.Equal(t, "TypeError: boom", resp.GetValues()[0].GetError())
		require.Equal(t, "32", resp.GetValues()[1].GetOutput().GetValue())
	})
}
