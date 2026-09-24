package auth_apikey

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/auth/permission"
	"github.com/fishtre-compagnie/husonym/backend/internal/utils"
	pkg_utils "github.com/fishtre-compagnie/husonym/backend/pkg/utils"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

type TokenContextKey struct{}
type TokenContextData struct {
	RawToken   string
	ApiKey     *db_queries.HusonymApiAccountApiKey
	ApiKeyType apikey.ApiKeyType
}

var (
	ErrInvalidApiKey = errors.New("token is not a valid husonym api key")
	ErrApiKeyExpired = husonymerrors.NewUnauthenticated("token is expired")
)

type Queries interface {
	GetAccountApiKeyByKeyValue(
		ctx context.Context,
		db db_queries.DBTX,
		apiKey string,
	) (db_queries.HusonymApiAccountApiKey, error)
}

type Client struct {
	q                       Queries
	db                      db_queries.DBTX
	allowedWorkerApiKeys    []string
	allowedWorkerProcedures map[string]any
}

func New(
	queries Queries,
	db db_queries.DBTX,
	allowedWorkerApiKeys []string,
	allowedWorkerProcedures []string,
) *Client {
	allowedWorkerProcedureSet := map[string]any{}
	for _, procedure := range allowedWorkerProcedures {
		allowedWorkerProcedureSet[procedure] = struct{}{}
	}
	return &Client{
		q:                       queries,
		db:                      db,
		allowedWorkerApiKeys:    allowedWorkerApiKeys,
		allowedWorkerProcedures: allowedWorkerProcedureSet,
	}
}

func (c *Client) InjectTokenCtx(
	ctx context.Context,
	header http.Header,
	spec connect.Spec,
) (context.Context, error) {
	token, err := utils.GetBearerTokenFromHeader(header, "Authorization")
	if err != nil {
		return nil, err
	}

	if apikey.IsValidV1AccountKey(token) {
		hashedKeyValue := pkg_utils.ToSha256(
			token,
		)
		apiKey, err := c.q.GetAccountApiKeyByKeyValue(ctx, c.db, hashedKeyValue)
		if err != nil && !husonymdb.IsNoRows(err) {
			return nil, err
		} else if err != nil && husonymdb.IsNoRows(err) {
			return nil, ErrInvalidApiKey
		}

		if time.Now().After(apiKey.ExpiresAt.Time) {
			return nil, ErrApiKeyExpired
		}
		required, err := requiredBy(spec)
		if err != nil {
			return nil, err
		}
		if err := permission.NewScope(apiKey.Permissions).Require(required...); err != nil {
			return nil, err
		}

		return SetTokenData(ctx, &TokenContextData{
			RawToken:   token,
			ApiKey:     &apiKey,
			ApiKeyType: apikey.AccountApiKey,
		}), nil
	} else if apikey.IsValidV1WorkerKey(token) &&
		isApiKeyAllowed(c.allowedWorkerApiKeys, token) &&
		isProcedureAllowed(c.allowedWorkerProcedures, spec.Procedure) {
		return SetTokenData(ctx, &TokenContextData{
			RawToken:   token,
			ApiKey:     nil,
			ApiKeyType: apikey.WorkerApiKey,
		}), nil
	}
	return nil, ErrInvalidApiKey
}

// requiredBy reads what a procedure declares a scoped key must hold. A procedure that declares
// nothing is refused: it is an opening nobody chose, and a test keeps every procedure declared.
func requiredBy(spec connect.Spec) ([]mgmtv1alpha1.Permission, error) {
	if method, ok := spec.Schema.(protoreflect.MethodDescriptor); ok {
		if required, ok := Requires(method); ok {
			return required, nil
		}
	}
	return nil, husonymerrors.NewUnauthorized(fmt.Sprintf(
		"%s declares no permission, so no API key may call it", spec.Procedure,
	))
}

// Requires gives what a procedure declares a scoped key must hold, and whether it declares
// anything at all. A procedure that needs no permission declares an empty list.
func Requires(method protoreflect.MethodDescriptor) ([]mgmtv1alpha1.Permission, bool) {
	opts, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok || opts == nil || !proto.HasExtension(opts, mgmtv1alpha1.E_Requires) {
		return nil, false
	}
	declared, ok := proto.GetExtension(opts, mgmtv1alpha1.E_Requires).(*mgmtv1alpha1.ProcedurePermissions)
	if !ok {
		return nil, false
	}
	if declared.GetNone() {
		return []mgmtv1alpha1.Permission{}, len(declared.GetAllOf()) == 0
	}
	return declared.GetAllOf(), len(declared.GetAllOf()) > 0
}

func GetTokenDataFromCtx(ctx context.Context) (*TokenContextData, error) {
	data, ok := ctx.Value(TokenContextKey{}).(*TokenContextData)
	if !ok {
		return nil, husonymerrors.NewUnauthenticated(
			"ctx does not contain TokenContextData or unable to cast struct",
		)
	}
	return data, nil
}

func SetTokenData(ctx context.Context, data *TokenContextData) context.Context {
	return context.WithValue(ctx, TokenContextKey{}, data)
}

func isApiKeyAllowed(allowedKeys []string, key string) bool {
	for _, allowedKey := range allowedKeys {
		if secureCompare(allowedKey, key) {
			return true
		}
	}
	return false
}

func isProcedureAllowed(allowedProcedures map[string]any, procedure string) bool {
	_, ok := allowedProcedures[procedure]
	return ok
}

func secureCompare(a, b string) bool {
	// Convert strings to byte slices for comparison
	aBytes := []byte(a)
	bBytes := []byte(b)

	// Check length first; if they differ, return false immediately
	if len(aBytes) != len(bBytes) {
		return false
	}

	// Use ConstantTimeCompare for a timing-attack resistant comparison
	return subtle.ConstantTimeCompare(aBytes, bBytes) == 1
}
