package authlogging_interceptor

import (
	"context"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/backend/internal/auth/tokenctx"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

type Interceptor struct {
	db     *husonymdb.HusonymDb
	onUser func(ctx context.Context, userId string)
}

// Option configures the interceptor.
type Option func(*Interceptor)

// WithOnUser calls fn with the internal id of a user authenticated by JWT, once it is known. It
// is not called for an API key, nor when the user cannot be resolved. fn runs on the request
// path: it must return at once and must not fail the request.
func WithOnUser(fn func(ctx context.Context, userId string)) Option {
	return func(i *Interceptor) { i.onUser = fn }
}

func NewInterceptor(db *husonymdb.HusonymDb, opts ...Option) connect.Interceptor {
	i := &Interceptor{db: db}
	for _, opt := range opts {
		opt(i)
	}
	return i
}

func (i *Interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
		return next(i.setAuthValues(ctx), request)
	}
}

func (i *Interceptor) WrapStreamingClient(
	next connect.StreamingClientFunc,
) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		return next(ctx, spec)
	}
}

func (i *Interceptor) WrapStreamingHandler(
	next connect.StreamingHandlerFunc,
) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		return next(i.setAuthValues(ctx), conn)
	}
}

func (i *Interceptor) setAuthValues(ctx context.Context) context.Context {
	vals, jwtUserId := resolveAuth(ctx, i.db)
	if jwtUserId != "" && i.onUser != nil {
		i.onUser(ctx, jwtUserId)
	}
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx).With(vals...)
	return logger_interceptor.SetLoggerContext(ctx, logger)
}

// resolveAuth returns the values that attribute a log line, and the internal id of the user
// when the caller is one authenticated by JWT and found.
func resolveAuth(ctx context.Context, db *husonymdb.HusonymDb) (values []any, jwtUserId string) {
	tokenCtxResp, err := tokenctx.GetTokenCtx(ctx)
	if err != nil {
		return []any{}, ""
	}
	output := []any{}

	if tokenCtxResp.JwtContextData != nil {
		output = append(output, "authUserId", tokenCtxResp.JwtContextData.AuthUserId)

		// By the pair, not by the subject: since identities carry their issuer, the same
		// subject can belong to two people, and a lookup on it alone would attribute a
		// line to whichever of them the database happened to return first.
		association, err := db.Q.GetUserAssociationByIdentity(ctx, db.Db, db_queries.GetUserAssociationByIdentityParams{
			ProviderSub: tokenCtxResp.JwtContextData.AuthUserId,
			ProviderIss: tokenCtxResp.JwtContextData.AuthIssuer,
		})
		if err == nil {
			jwtUserId = husonymdb.UUIDString(association.UserID)
			output = append(output, "userId", jwtUserId)
		}
	} else if tokenCtxResp.ApiKeyContextData != nil {
		output = append(output, "apiKeyType", tokenCtxResp.ApiKeyContextData.ApiKeyType)
		if tokenCtxResp.ApiKeyContextData.ApiKey != nil {
			output = append(output,
				"apiKeyId", husonymdb.UUIDString(tokenCtxResp.ApiKeyContextData.ApiKey.ID),
				"accountId", husonymdb.UUIDString(tokenCtxResp.ApiKeyContextData.ApiKey.AccountID),
				"userId", husonymdb.UUIDString(tokenCtxResp.ApiKeyContextData.ApiKey.UserID),
			)
		}
	}
	return output, jwtUserId
}
