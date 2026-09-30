package husonym_benthos_gcs

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/cloudidentity"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	"github.com/redpanda-data/benthos/v4/public/service"
	"github.com/stretchr/testify/require"
)

// serviceAccountKey is the key file of a service account nobody has, with the given fields
// replaced.
func serviceAccountKey(t *testing.T, replaced ...string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	file := map[string]string{
		"type":           "service_account",
		"project_id":     "project",
		"private_key_id": "key",
		"private_key":    string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"client_email":   "writer@project.iam.gserviceaccount.com",
		"client_id":      "1",
		"token_uri":      "https://oauth2.googleapis.com/token",
	}
	for i := 0; i+1 < len(replaced); i += 2 {
		file[replaced[i]] = replaced[i+1]
	}
	bits, err := json.Marshal(file)
	require.NoError(t, err)
	return string(bits)
}

func connectionsOf(credentials *string) func(string) (connectionmanager.ConnectionInput, error) {
	return func(id string) (connectionmanager.ConnectionInput, error) {
		if id != "gcs" {
			return nil, errors.New("unknown connection")
		}
		return &mgmtv1alpha1.Connection{Id: id, ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_GcpCloudstorageConfig{
				GcpCloudstorageConfig: &mgmtv1alpha1.GcpCloudStorageConnectionConfig{
					Bucket: "the-bucket", ServiceAccountCredentials: credentials,
				},
			},
		}}, nil
	}
}

func parse(t *testing.T) *service.ParsedConfig {
	t.Helper()
	conf, err := outputSpec().ParseYAML(`
connection_id: gcs
path: 'objects/${! meta("n") }.txt.gz'
content_type: txt/plain
content_encoding: gzip
`, service.NewEnvironment())
	require.NoError(t, err)
	return conf
}

// The writer acts with the service account of its connection. Without one, it would act with
// the application default credentials of the worker: refused unless the deployment allows it.
// Credentials of another type than a service account may name a file or a URL the worker would
// read: refused.
func Test_GcsWriter_TheCredentialsOfItsConnection(t *testing.T) {
	key := serviceAccountKey(t)
	writer, err := newGcsWriter(parse(t), connectionsOf(&key), cloudidentity.Policy{})
	require.NoError(t, err)
	require.NoError(t, writer.Close(context.Background()))

	_, err = newGcsWriter(parse(t), connectionsOf(nil), cloudidentity.Policy{})
	require.ErrorContains(t, err, cloudidentity.Variable)

	external := `{"type": "external_account", "audience": "a", "subject_token_type": "t", "token_url": "https://sts.googleapis.com/v1/token", "credential_source": {"file": "/etc/passwd"}}`
	_, err = newGcsWriter(parse(t), connectionsOf(&external), cloudidentity.Policy{})
	require.ErrorContains(t, err, "only a service account")

	// A key naming another token endpoint would have the worker post to it.
	elsewhere := serviceAccountKey(t, "token_uri", "http://internal-service:8080/")
	_, err = newGcsWriter(parse(t), connectionsOf(&elsewhere), cloudidentity.Policy{})
	require.ErrorContains(t, err, "Google's token endpoint")
	otherUniverse := serviceAccountKey(t, "universe_domain", "example.com")
	_, err = newGcsWriter(parse(t), connectionsOf(&otherUniverse), cloudidentity.Policy{})
	require.ErrorContains(t, err, "Google's own universe")
}

type recordedObject struct{ name, contentType, contentEncoding, body string }

type fakeStore struct{ objects []recordedObject }

func (f *fakeStore) put(_ context.Context, name, contentType, contentEncoding string, body []byte) error {
	f.objects = append(f.objects, recordedObject{name, contentType, contentEncoding, string(body)})
	return nil
}
func (f *fakeStore) close() error { return nil }

// Each message becomes an object named by the path, with the content type and encoding asked.
func Test_GcsWriter_WritesEachMessage(t *testing.T) {
	key := serviceAccountKey(t)
	writer, err := newGcsWriter(parse(t), connectionsOf(&key), cloudidentity.Policy{})
	require.NoError(t, err)
	store := &fakeStore{}
	writer.store = store

	batch := service.MessageBatch{service.NewMessage([]byte("first")), service.NewMessage([]byte("second"))}
	batch[0].MetaSet("n", "1")
	batch[1].MetaSet("n", "2")
	require.NoError(t, writer.WriteBatch(context.Background(), batch))
	require.Equal(t, []recordedObject{
		{"objects/1.txt.gz", "txt/plain", "gzip", "first"},
		{"objects/2.txt.gz", "txt/plain", "gzip", "second"},
	}, store.objects)

	require.NoError(t, writer.Close(context.Background()))
	require.ErrorIs(t, writer.WriteBatch(context.Background(), batch), service.ErrNotConnected)
}
