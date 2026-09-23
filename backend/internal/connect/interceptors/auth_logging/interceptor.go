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
	db *husonymdb.HusonymDb
}

func NewInterceptor(db *husonymdb.HusonymDb) connect.Interceptor {
	return &Interceptor{db: db}
}

func (i *Interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
		return next(setAuthValues(ctx, i.db), request)
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
		return next(setAuthValues(ctx, i.db), conn)
	}
}

func setAuthValues(ctx context.Context, db *husonymdb.HusonymDb) context.Context {
	vals := getAuthValues(ctx, db)
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx).With(vals...)
	return logger_interceptor.SetLoggerContext(ctx, logger)
}

func getAuthValues(ctx context.Context, db *husonymdb.HusonymDb) []any {
	tokenCtxResp, err := tokenctx.GetTokenCtx(ctx)
	if err != nil {
		return []any{}
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
			output = append(output, "userId", husonymdb.UUIDString(association.UserID))
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
	return output
}
