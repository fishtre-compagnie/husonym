package v1alpha_anonymizationservice

import (
	"net/http"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/piitext"
	"github.com/stretchr/testify/require"
)

func headerWith(value string) http.Header {
	header := http.Header{}
	header.Set(piitext.HashKeyHeader, value)
	return header
}

// The key a run hands with its calls is read from the worker alone. What tells the worker from
// the other callers is userdata.WorkerOnly, tested where it lives: with authentication the key
// of the worker alone passes, and a caller in a session, as here, does not.
func Test_runHashKey(t *testing.T) {
	key := piitext.HashKey{1, 2, 3}
	notTheWorker := &userdata.User{}
	withAuthentication := &Service{cfg: &Config{WorkerOnly: userdata.WorkerOnly{IsAuthEnabled: true}}}
	// Without authentication no caller is told from the worker.
	asTheWorker := &Service{cfg: &Config{WorkerOnly: userdata.WorkerOnly{}}}

	t.Run("the key of the worker is the key of the request", func(t *testing.T) {
		got, err := asTheWorker.runHashKey(notTheWorker, headerWith(key.Encode()))
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, key, *got)
	})

	t.Run("a request without the header has no key", func(t *testing.T) {
		got, err := asTheWorker.runHashKey(notTheWorker, http.Header{})
		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("a key the worker sends that cannot be read is refused", func(t *testing.T) {
		_, err := asTheWorker.runHashKey(notTheWorker, headerWith("not a key"))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("the header of a caller that is not the worker is not read", func(t *testing.T) {
		got, err := withAuthentication.runHashKey(notTheWorker, headerWith(key.Encode()))
		require.NoError(t, err)
		require.Nil(t, got)

		got, err = withAuthentication.runHashKey(notTheWorker, headerWith("not a key"))
		require.NoError(t, err)
		require.Nil(t, got)
	})
}

func Test_validateTransformerConfig(t *testing.T) {
	piiText := func(config *mgmtv1alpha1.TransformPiiText) *mgmtv1alpha1.TransformerConfig {
		return &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{TransformPiiTextConfig: config},
		}
	}
	nested := &mgmtv1alpha1.PiiAnonymizer{Config: &mgmtv1alpha1.PiiAnonymizer_Transform_{
		Transform: &mgmtv1alpha1.PiiAnonymizer_Transform{Config: piiText(&mgmtv1alpha1.TransformPiiText{})},
	}}

	t.Run("no config is refused", func(t *testing.T) {
		require.Error(t, validateTransformerConfig(nil))
	})

	t.Run("another transformer and a plain PII text one pass", func(t *testing.T) {
		require.NoError(t, validateTransformerConfig(&mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
		}))
		require.NoError(t, validateTransformerConfig(piiText(&mgmtv1alpha1.TransformPiiText{})))
	})

	t.Run("a PII text transformer nested in its default anonymizer is a bad request", func(t *testing.T) {
		err := validateTransformerConfig(piiText(&mgmtv1alpha1.TransformPiiText{DefaultAnonymizer: nested}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		require.ErrorContains(t, err,
			"found nested TransformPiiText config in default anonymizer. TransformPiiText may not be used deeply nested within itself.")
	})

	t.Run("a PII text transformer nested in an entity anonymizer is a bad request", func(t *testing.T) {
		err := validateTransformerConfig(piiText(&mgmtv1alpha1.TransformPiiText{
			EntityAnonymizers: map[string]*mgmtv1alpha1.PiiAnonymizer{"PERSON": nested},
		}))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		require.ErrorContains(t, err,
			"found nested TransformPiiText config in entity (PERSON) anonymizer. TransformPiiText may not be used deeply nested within itself.")
	})
}
