// Package licenserefusal counts a license refusal, in the one way every place that refuses does.
package licenserefusal

import (
	"context"
	"time"

	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/google/uuid"
)

// Counter keeps the count of the refusals. *usagestore.Store is one.
type Counter interface {
	CountRefusal(ctx context.Context, accountId string, gates []license.Gate, at time.Time) error
}

// countTimeout bounds a count: a refused answer is not held by a database that does not answer.
var countTimeout = 2 * time.Second

// Count counts the refusal of the account by the gates, today. It never fails the caller: a
// refusal that names no account, an account that is not a uuid, or no gate is skipped, a service
// built without a counter counts nothing, and a count that fails or does not finish within the
// bound is logged. The count outlives a caller that gave up on its call.
func Count(ctx context.Context, counter Counter, accountId string, gates []license.Gate) {
	if counter == nil {
		return
	}
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	if accountId == "" || len(gates) == 0 {
		logger.DebugContext(ctx, "a license refusal was not counted: it names no account or no gate")
		return
	}
	if _, err := uuid.Parse(accountId); err != nil {
		logger.DebugContext(ctx, "a license refusal was not counted: the account id is not a uuid")
		return
	}
	countCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), countTimeout)
	defer cancel()
	if err := counter.CountRefusal(countCtx, accountId, gates, time.Now()); err != nil {
		logger.WarnContext(ctx, "unable to count a license refusal", "error", err.Error())
	}
}
