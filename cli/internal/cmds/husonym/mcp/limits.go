package mcp_cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
)

// timeLimits is how long each call to the API is waited for. Without one, a call the API
// never answers never ends: the agent waits on its tool, and a job being run or changed stays
// taken by that call.
type timeLimits struct {
	// byDefault is the limit of a call the API answers from what it holds itself.
	byDefault time.Duration
	// slow holds, by procedure, the limit of the calls that take longer.
	slow map[string]time.Duration
}

const (
	// apiTimeLimit is the limit of a call the API answers from what it holds itself: its own
	// database, the orchestrator.
	apiTimeLimit = 30 * time.Second
	// databaseTimeLimit is the limit of a call the API answers by reading the database of a
	// connection, which may be far, slow or large.
	databaseTimeLimit = 2 * time.Minute
	// checkTimeLimit is a little longer than the two minutes the API gives the check of a
	// connection, so that the API says it did not end in time, rather than the call being cut
	// short.
	checkTimeLimit = 2*time.Minute + 15*time.Second
)

// apiTimeLimits are the limits of the calls the server makes. The pre-flight check is not
// among them: its reader gives it its own, which is kept.
func apiTimeLimits() timeLimits {
	return timeLimits{
		byDefault: apiTimeLimit,
		slow: map[string]time.Duration{
			mgmtv1alpha1connect.ConnectionDataServiceGetAllSchemasAndTablesProcedure:        databaseTimeLimit,
			mgmtv1alpha1connect.ConnectionDataServiceGetConnectionSchemaProcedure:           databaseTimeLimit,
			mgmtv1alpha1connect.ConnectionDataServiceGetConnectionTableConstraintsProcedure: databaseTimeLimit,
			mgmtv1alpha1connect.ConnectionDataServiceDetectPiiInConnectionDataProcedure:     databaseTimeLimit,
			mgmtv1alpha1connect.ConnectionDataServicePreviewColumnTransformerProcedure:      databaseTimeLimit,
			mgmtv1alpha1connect.JobServiceValidateJobMappingsProcedure:                      databaseTimeLimit,
			mgmtv1alpha1connect.ConnectionServiceCheckConnectionConfigByIdProcedure:         checkTimeLimit,
		},
	}
}

func (l timeLimits) of(procedure string) time.Duration {
	if limit, ok := l.slow[procedure]; ok {
		return limit
	}
	return l.byDefault
}

// interceptor gives each call its limit. A call that comes with a deadline keeps it. The API
// is told the limit, and stops working on the call when it is reached.
func (l timeLimits) interceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if _, limited := ctx.Deadline(); limited {
				return next(ctx, req)
			}
			limit := l.of(req.Spec().Procedure)
			limitedCtx, cancel := context.WithTimeout(ctx, limit)
			defer cancel()
			res, err := next(limitedCtx, req)
			// Cut short at the limit, and not by the caller giving up: said in words the agent
			// can act on.
			if err != nil && errors.Is(limitedCtx.Err(), context.DeadlineExceeded) {
				return nil, connect.NewError(
					connect.CodeDeadlineExceeded,
					fmt.Errorf("the API did not answer within %s", limit),
				)
			}
			return res, err
		}
	}
}
