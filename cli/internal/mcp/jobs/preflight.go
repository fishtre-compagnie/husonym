package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// preflightWait is how long a pre-flight check is waited for: a little longer than the API
// gives it, so that the API says it did not end in time, rather than the call being cut short.
const preflightWait = 3*time.Minute + 15*time.Second

// Preflight tells what a run of a job would meet, were it started now. Nothing is read from
// the tables nor written. The report is configuration and privileges, named after the tables
// and columns of the databases; never a value from a row.
//
// Why a check did not end is said in the API's own words only where the API writes them
// itself. A failure of the worker comes back with its reason, which is often the error of a
// database driver and may quote where and how it connects; a call cut short by its deadline
// may carry the error it was cut in. Both are replaced here.
func (r *Reader) Preflight(ctx context.Context, jobId string) (*mgmtv1alpha1.PreflightJobResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, preflightWait)
	defer cancel()
	res, err := r.client.PreflightJob(ctx, connect.NewRequest(&mgmtv1alpha1.PreflightJobRequest{JobId: jobId}))
	if err != nil {
		return nil, preflightError(err)
	}
	return res.Msg, nil
}

func preflightError(err error) error {
	// Cut short here or at the API, a call may carry the error it was cut in.
	if connect.CodeOf(err) == connect.CodeDeadlineExceeded {
		return errors.New("the pre-flight check did not end in time")
	}
	if !connect.IsWireError(err) {
		return fmt.Errorf("the API could not be reached (%s)", connect.CodeOf(err))
	}
	switch connect.CodeOf(err) {
	case connect.CodeInvalidArgument,
		connect.CodeNotFound,
		connect.CodePermissionDenied,
		connect.CodeUnauthenticated,
		connect.CodeFailedPrecondition:
		return err
	case connect.CodeUnavailable:
		return errors.New(
			"the pre-flight check could not end: a connection of the job is out of reach or refused the " +
				"worker, or the job refers to something gone, such as a connection or a transformer. " +
				"check_connection says whether each connection answers; the page of the job in the UI shows why",
		)
	default:
		return fmt.Errorf("the pre-flight check could not end (%s)", connect.CodeOf(err))
	}
}
