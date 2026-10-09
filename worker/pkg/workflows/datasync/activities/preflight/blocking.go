package preflight_activity

import (
	"fmt"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/preflight"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared/runerror"
	"go.temporal.io/sdk/temporal"
)

// errorTypeBlocking is the type of the error of a run its pre-flight check stops.
const errorTypeBlocking = "PreflightBlocking"

// blockingCategories gives the category of the error of a run stopped on a finding, by the
// kind of the finding. A kind that is not here has none: what the plan of the job tells, and
// an engine that cannot run it, are no error of a database.
var blockingCategories = map[preflight.Kind]mgmtv1alpha1.RunErrorCategory{
	// the table, or a column the run writes, is not there
	mgmtv1alpha1.PreflightFinding_KIND_TABLE_EXISTS: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OBJECT_MISSING,
	// the account of a connection lacks what its role in the job needs; a server that only
	// reads refuses the writes as an account without the grant is refused them
	mgmtv1alpha1.PreflightFinding_KIND_READABLE:               mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES,
	mgmtv1alpha1.PreflightFinding_KIND_SERVER_WRITABLE:        mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES,
	mgmtv1alpha1.PreflightFinding_KIND_WRITABLE:               mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES,
	mgmtv1alpha1.PreflightFinding_KIND_TRUNCATE:               mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES,
	mgmtv1alpha1.PreflightFinding_KIND_TRIGGERS:               mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES,
	mgmtv1alpha1.PreflightFinding_KIND_TRIGGER_DEFINER:        mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES,
	mgmtv1alpha1.PreflightFinding_KIND_FOREIGN_KEY_SUSPENSION: mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES,
}

// blockingCategory gives the category a run stopped on these findings is told with: that of
// the first of them, in the order of the report, whose kind has one, else "other". It reads
// the kind of a finding, never its sentence.
func blockingCategory(blocking []*preflight.Finding) mgmtv1alpha1.RunErrorCategory {
	for _, finding := range blocking {
		if category, ok := blockingCategories[finding.Kind]; ok {
			return category
		}
	}
	return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER
}

// blockingError is the error of a run its pre-flight check stops, which tells the category
// of what stopped it. Asking again finds the same: the job, or the grants, have to change.
func blockingError(blocking []*preflight.Finding) error {
	stopped := temporal.NewNonRetryableApplicationError(
		fmt.Sprintf("pre-flight check stopped the run: %s", strings.Join(preflight.Messages(blocking), "; ")),
		errorTypeBlocking, nil)
	return runerror.Tell(stopped, blockingCategory(blocking))
}
