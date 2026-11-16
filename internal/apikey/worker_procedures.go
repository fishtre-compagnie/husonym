package apikey

import "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"

// WorkerProcedures are the procedures a worker key opens: what the worker calls, and nothing
// more. A worker key passes neither RBAC nor the scope of an account key, so a procedure lands
// here only because the worker needs it; worker_procedures_test.go checks that every procedure
// the worker's code calls is here.
var WorkerProcedures = []string{
	mgmtv1alpha1connect.JobServiceGetJobProcedure,
	mgmtv1alpha1connect.JobServiceGetRunContextProcedure,
	mgmtv1alpha1connect.JobServiceSetRunContextProcedure,
	mgmtv1alpha1connect.JobServiceSetRunContextsProcedure,
	mgmtv1alpha1connect.JobServiceReconcileJobMappingsProcedure,
	mgmtv1alpha1connect.JobServiceGetActiveJobHooksByTimingProcedure,
	mgmtv1alpha1connect.ConnectionServiceGetConnectionProcedure,
	mgmtv1alpha1connect.ConnectionDataServiceGetConnectionInitStatementsProcedure,
	mgmtv1alpha1connect.ConnectionDataServiceGetConnectionDataStreamProcedure,
	// The PII detection job asks the API to analyze the content of the free-text columns of a table.
	mgmtv1alpha1connect.ConnectionDataServiceDetectPiiInConnectionDataProcedure,
	mgmtv1alpha1connect.TransformersServiceGetUserDefinedTransformerByIdProcedure,
	mgmtv1alpha1connect.UserAccountServiceIsAccountStatusValidProcedure,
	mgmtv1alpha1connect.UserAccountServiceGetBillingAccountsProcedure,
	mgmtv1alpha1connect.UserAccountServiceSetBillingMeterEventProcedure,
	mgmtv1alpha1connect.UserAccountServiceGetSystemLicenseKeyProcedure,
	mgmtv1alpha1connect.AccountSettingServiceGetAccountConsistencyKeyProcedure,
	mgmtv1alpha1connect.MetricsServiceGetDailyMetricCountProcedure,
	mgmtv1alpha1connect.AnonymizationServiceAnonymizeSingleProcedure,
	mgmtv1alpha1connect.AnonymizationServiceAnonymizeManyProcedure,
	mgmtv1alpha1connect.AccountHookServiceGetActiveAccountHooksByEventProcedure,
	mgmtv1alpha1connect.AccountHookServiceGetAccountHookProcedure,
	mgmtv1alpha1connect.UsageServiceRecordRunStartedProcedure,
	mgmtv1alpha1connect.UsageServiceRecordRunEndedProcedure,
}
