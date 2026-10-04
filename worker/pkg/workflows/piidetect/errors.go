package piidetect

import (
	"errors"
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/model"
	"go.temporal.io/sdk/temporal"
)

// The types of the errors that are not retried, and of the failure of an incomplete run.
// They name the cause in the history of the workflow.
const (
	errorTypeNotPiiDetectJob   = "NotPiiDetectJob"
	errorTypeUnsupportedSource = "UnsupportedSource"
	errorTypeModelRejected     = "ModelRejected"
	errorTypeIncompleteScan    = "ScanIncomplete"
)

// errUnsupportedSource is the error for a source the job cannot scan: only the tables
// of a PostgreSQL, MySQL or SQL Server database are. A new attempt would not change it.
func errUnsupportedSource(kind string) error {
	return temporal.NewNonRetryableApplicationError(
		fmt.Sprintf(
			"a PII detection job scans the tables of a PostgreSQL, MySQL or SQL Server database: its source is %s",
			kind,
		),
		errorTypeUnsupportedSource,
		nil,
	)
}

// scannableConnection tells whether the tables of a connection can be scanned, and names
// its kind when they cannot.
func scannableConnection(connection *mgmtv1alpha1.Connection) (kind string, ok bool) {
	switch connection.GetConnectionConfig().GetConfig().(type) {
	case *mgmtv1alpha1.ConnectionConfig_PgConfig,
		*mgmtv1alpha1.ConnectionConfig_MysqlConfig,
		*mgmtv1alpha1.ConnectionConfig_MssqlConfig:
		return "", true
	case *mgmtv1alpha1.ConnectionConfig_MongoConfig:
		return "a MongoDB database", false
	case *mgmtv1alpha1.ConnectionConfig_DynamodbConfig:
		return "a DynamoDB database", false
	case *mgmtv1alpha1.ConnectionConfig_AwsS3Config, *mgmtv1alpha1.ConnectionConfig_GcpCloudstorageConfig:
		return "an object storage", false
	case *mgmtv1alpha1.ConnectionConfig_OpenaiConfig:
		return "a language model", false
	}
	return "of a kind that has no table to read", false
}

// modelError is the error of the model activity for a request that failed. A failure
// that a new attempt cannot cure is not retried, and the error says so itself: it knows
// the cause, whatever retry policy the activity was scheduled with.
func modelError(err error) error {
	var failure *model.Error
	if errors.As(err, &failure) && failure.Permanent() {
		return temporal.NewNonRetryableApplicationError(
			"the model could not be asked",
			errorTypeModelRejected,
			failure,
		)
	}
	return fmt.Errorf("the model could not be asked: %w", err)
}
