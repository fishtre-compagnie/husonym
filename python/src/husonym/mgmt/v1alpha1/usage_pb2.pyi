import datetime

from buf.validate import validate_pb2 as _validate_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from mgmt.v1alpha1 import job_pb2 as _job_pb2
from mgmt.v1alpha1 import metrics_pb2 as _metrics_pb2
from mgmt.v1alpha1 import permission_pb2 as _permission_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class RunOutcome(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    RUN_OUTCOME_UNSPECIFIED: _ClassVar[RunOutcome]
    RUN_OUTCOME_COMPLETED: _ClassVar[RunOutcome]
    RUN_OUTCOME_FAILED: _ClassVar[RunOutcome]
    RUN_OUTCOME_CANCELED: _ClassVar[RunOutcome]

class RunErrorCategory(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    RUN_ERROR_CATEGORY_UNSPECIFIED: _ClassVar[RunErrorCategory]
    RUN_ERROR_CATEGORY_CONNECTION_REFUSED: _ClassVar[RunErrorCategory]
    RUN_ERROR_CATEGORY_AUTHENTICATION_REFUSED: _ClassVar[RunErrorCategory]
    RUN_ERROR_CATEGORY_TIMEOUT: _ClassVar[RunErrorCategory]
    RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED: _ClassVar[RunErrorCategory]
    RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES: _ClassVar[RunErrorCategory]
    RUN_ERROR_CATEGORY_OBJECT_MISSING: _ClassVar[RunErrorCategory]
    RUN_ERROR_CATEGORY_TYPE_MISMATCH: _ClassVar[RunErrorCategory]
    RUN_ERROR_CATEGORY_RESOURCES_EXHAUSTED: _ClassVar[RunErrorCategory]
    RUN_ERROR_CATEGORY_CANCELED: _ClassVar[RunErrorCategory]
    RUN_ERROR_CATEGORY_LICENSE: _ClassVar[RunErrorCategory]
    RUN_ERROR_CATEGORY_OTHER: _ClassVar[RunErrorCategory]

class RunErrorStep(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    RUN_ERROR_STEP_UNSPECIFIED: _ClassVar[RunErrorStep]
    RUN_ERROR_STEP_PREFLIGHT: _ClassVar[RunErrorStep]
    RUN_ERROR_STEP_SCHEMA_INIT: _ClassVar[RunErrorStep]
    RUN_ERROR_STEP_TABLE_SYNC: _ClassVar[RunErrorStep]
    RUN_ERROR_STEP_HOOKS: _ClassVar[RunErrorStep]
    RUN_ERROR_STEP_INTEGRITY_CHECK: _ClassVar[RunErrorStep]
    RUN_ERROR_STEP_OTHER: _ClassVar[RunErrorStep]

class UsageReportingMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    USAGE_REPORTING_MODE_UNSPECIFIED: _ClassVar[UsageReportingMode]
    USAGE_REPORTING_MODE_ONLINE: _ClassVar[UsageReportingMode]
    USAGE_REPORTING_MODE_OFFLINE_REPORT: _ClassVar[UsageReportingMode]
    USAGE_REPORTING_MODE_NONE: _ClassVar[UsageReportingMode]

class UsageReportStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    USAGE_REPORT_STATUS_UNSPECIFIED: _ClassVar[UsageReportStatus]
    USAGE_REPORT_STATUS_KEPT: _ClassVar[UsageReportStatus]
    USAGE_REPORT_STATUS_TO_BE_SENT: _ClassVar[UsageReportStatus]
    USAGE_REPORT_STATUS_SENT: _ClassVar[UsageReportStatus]
    USAGE_REPORT_STATUS_NOT_SENT: _ClassVar[UsageReportStatus]

class JobKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    JOB_KIND_UNSPECIFIED: _ClassVar[JobKind]
    JOB_KIND_SYNC: _ClassVar[JobKind]
    JOB_KIND_GENERATE: _ClassVar[JobKind]
    JOB_KIND_AI_GENERATE: _ClassVar[JobKind]
    JOB_KIND_PII_DETECT: _ClassVar[JobKind]
RUN_OUTCOME_UNSPECIFIED: RunOutcome
RUN_OUTCOME_COMPLETED: RunOutcome
RUN_OUTCOME_FAILED: RunOutcome
RUN_OUTCOME_CANCELED: RunOutcome
RUN_ERROR_CATEGORY_UNSPECIFIED: RunErrorCategory
RUN_ERROR_CATEGORY_CONNECTION_REFUSED: RunErrorCategory
RUN_ERROR_CATEGORY_AUTHENTICATION_REFUSED: RunErrorCategory
RUN_ERROR_CATEGORY_TIMEOUT: RunErrorCategory
RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED: RunErrorCategory
RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES: RunErrorCategory
RUN_ERROR_CATEGORY_OBJECT_MISSING: RunErrorCategory
RUN_ERROR_CATEGORY_TYPE_MISMATCH: RunErrorCategory
RUN_ERROR_CATEGORY_RESOURCES_EXHAUSTED: RunErrorCategory
RUN_ERROR_CATEGORY_CANCELED: RunErrorCategory
RUN_ERROR_CATEGORY_LICENSE: RunErrorCategory
RUN_ERROR_CATEGORY_OTHER: RunErrorCategory
RUN_ERROR_STEP_UNSPECIFIED: RunErrorStep
RUN_ERROR_STEP_PREFLIGHT: RunErrorStep
RUN_ERROR_STEP_SCHEMA_INIT: RunErrorStep
RUN_ERROR_STEP_TABLE_SYNC: RunErrorStep
RUN_ERROR_STEP_HOOKS: RunErrorStep
RUN_ERROR_STEP_INTEGRITY_CHECK: RunErrorStep
RUN_ERROR_STEP_OTHER: RunErrorStep
USAGE_REPORTING_MODE_UNSPECIFIED: UsageReportingMode
USAGE_REPORTING_MODE_ONLINE: UsageReportingMode
USAGE_REPORTING_MODE_OFFLINE_REPORT: UsageReportingMode
USAGE_REPORTING_MODE_NONE: UsageReportingMode
USAGE_REPORT_STATUS_UNSPECIFIED: UsageReportStatus
USAGE_REPORT_STATUS_KEPT: UsageReportStatus
USAGE_REPORT_STATUS_TO_BE_SENT: UsageReportStatus
USAGE_REPORT_STATUS_SENT: UsageReportStatus
USAGE_REPORT_STATUS_NOT_SENT: UsageReportStatus
JOB_KIND_UNSPECIFIED: JobKind
JOB_KIND_SYNC: JobKind
JOB_KIND_GENERATE: JobKind
JOB_KIND_AI_GENERATE: JobKind
JOB_KIND_PII_DETECT: JobKind

class RecordRunStartedRequest(_message.Message):
    __slots__ = ("job_id", "run_id", "started_at")
    JOB_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    job_id: str
    run_id: str
    started_at: _timestamp_pb2.Timestamp
    def __init__(self, job_id: _Optional[str] = ..., run_id: _Optional[str] = ..., started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class RecordRunStartedResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class RecordRunEndedRequest(_message.Message):
    __slots__ = ("job_id", "run_id", "started_at", "ended_at", "outcome", "rows_read", "rows_discarded", "retries", "tables_uncounted", "source_version_major", "error_category", "error_step")
    JOB_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    ENDED_AT_FIELD_NUMBER: _ClassVar[int]
    OUTCOME_FIELD_NUMBER: _ClassVar[int]
    ROWS_READ_FIELD_NUMBER: _ClassVar[int]
    ROWS_DISCARDED_FIELD_NUMBER: _ClassVar[int]
    RETRIES_FIELD_NUMBER: _ClassVar[int]
    TABLES_UNCOUNTED_FIELD_NUMBER: _ClassVar[int]
    SOURCE_VERSION_MAJOR_FIELD_NUMBER: _ClassVar[int]
    ERROR_CATEGORY_FIELD_NUMBER: _ClassVar[int]
    ERROR_STEP_FIELD_NUMBER: _ClassVar[int]
    job_id: str
    run_id: str
    started_at: _timestamp_pb2.Timestamp
    ended_at: _timestamp_pb2.Timestamp
    outcome: RunOutcome
    rows_read: int
    rows_discarded: int
    retries: int
    tables_uncounted: int
    source_version_major: str
    error_category: RunErrorCategory
    error_step: RunErrorStep
    def __init__(self, job_id: _Optional[str] = ..., run_id: _Optional[str] = ..., started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., ended_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., outcome: _Optional[_Union[RunOutcome, str]] = ..., rows_read: _Optional[int] = ..., rows_discarded: _Optional[int] = ..., retries: _Optional[int] = ..., tables_uncounted: _Optional[int] = ..., source_version_major: _Optional[str] = ..., error_category: _Optional[_Union[RunErrorCategory, str]] = ..., error_step: _Optional[_Union[RunErrorStep, str]] = ...) -> None: ...

class RecordRunEndedResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetUsageReportingRequest(_message.Message):
    __slots__ = ("account_id",)
    ACCOUNT_ID_FIELD_NUMBER: _ClassVar[int]
    account_id: str
    def __init__(self, account_id: _Optional[str] = ...) -> None: ...

class GetUsageReportingResponse(_message.Message):
    __slots__ = ("license_mode", "mode", "below_license", "diagnostics", "sending_since", "first_send_at", "last_sent_at", "silent", "reports")
    LICENSE_MODE_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    BELOW_LICENSE_FIELD_NUMBER: _ClassVar[int]
    DIAGNOSTICS_FIELD_NUMBER: _ClassVar[int]
    SENDING_SINCE_FIELD_NUMBER: _ClassVar[int]
    FIRST_SEND_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_SENT_AT_FIELD_NUMBER: _ClassVar[int]
    SILENT_FIELD_NUMBER: _ClassVar[int]
    REPORTS_FIELD_NUMBER: _ClassVar[int]
    license_mode: UsageReportingMode
    mode: UsageReportingMode
    below_license: bool
    diagnostics: bool
    sending_since: _timestamp_pb2.Timestamp
    first_send_at: _timestamp_pb2.Timestamp
    last_sent_at: _timestamp_pb2.Timestamp
    silent: bool
    reports: _containers.RepeatedCompositeFieldContainer[UsageReportSummary]
    def __init__(self, license_mode: _Optional[_Union[UsageReportingMode, str]] = ..., mode: _Optional[_Union[UsageReportingMode, str]] = ..., below_license: _Optional[bool] = ..., diagnostics: _Optional[bool] = ..., sending_since: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., first_send_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., last_sent_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., silent: _Optional[bool] = ..., reports: _Optional[_Iterable[_Union[UsageReportSummary, _Mapping]]] = ...) -> None: ...

class UsageReportSummary(_message.Message):
    __slots__ = ("day", "status", "sent_at", "attempts")
    DAY_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    SENT_AT_FIELD_NUMBER: _ClassVar[int]
    ATTEMPTS_FIELD_NUMBER: _ClassVar[int]
    day: _metrics_pb2.Date
    status: UsageReportStatus
    sent_at: _timestamp_pb2.Timestamp
    attempts: int
    def __init__(self, day: _Optional[_Union[_metrics_pb2.Date, _Mapping]] = ..., status: _Optional[_Union[UsageReportStatus, str]] = ..., sent_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., attempts: _Optional[int] = ...) -> None: ...

class GetUsageReportRequest(_message.Message):
    __slots__ = ("account_id", "day")
    ACCOUNT_ID_FIELD_NUMBER: _ClassVar[int]
    DAY_FIELD_NUMBER: _ClassVar[int]
    account_id: str
    day: _metrics_pb2.Date
    def __init__(self, account_id: _Optional[str] = ..., day: _Optional[_Union[_metrics_pb2.Date, _Mapping]] = ...) -> None: ...

class GetUsageReportResponse(_message.Message):
    __slots__ = ("document", "seal", "key_fingerprint")
    DOCUMENT_FIELD_NUMBER: _ClassVar[int]
    SEAL_FIELD_NUMBER: _ClassVar[int]
    KEY_FINGERPRINT_FIELD_NUMBER: _ClassVar[int]
    document: str
    seal: str
    key_fingerprint: str
    def __init__(self, document: _Optional[str] = ..., seal: _Optional[str] = ..., key_fingerprint: _Optional[str] = ...) -> None: ...

class GetUsagePeriodReportRequest(_message.Message):
    __slots__ = ("account_id", "from_month", "to_month")
    ACCOUNT_ID_FIELD_NUMBER: _ClassVar[int]
    FROM_MONTH_FIELD_NUMBER: _ClassVar[int]
    TO_MONTH_FIELD_NUMBER: _ClassVar[int]
    account_id: str
    from_month: str
    to_month: str
    def __init__(self, account_id: _Optional[str] = ..., from_month: _Optional[str] = ..., to_month: _Optional[str] = ...) -> None: ...

class GetUsagePeriodReportResponse(_message.Message):
    __slots__ = ("document", "seal", "key_fingerprint")
    DOCUMENT_FIELD_NUMBER: _ClassVar[int]
    SEAL_FIELD_NUMBER: _ClassVar[int]
    KEY_FINGERPRINT_FIELD_NUMBER: _ClassVar[int]
    document: str
    seal: str
    key_fingerprint: str
    def __init__(self, document: _Optional[str] = ..., seal: _Optional[str] = ..., key_fingerprint: _Optional[str] = ...) -> None: ...

class UsageTotals(_message.Message):
    __slots__ = ("runs", "runs_completed", "runs_canceled", "rows_read", "rows_discarded", "runs_with_uncounted_rows")
    RUNS_FIELD_NUMBER: _ClassVar[int]
    RUNS_COMPLETED_FIELD_NUMBER: _ClassVar[int]
    RUNS_CANCELED_FIELD_NUMBER: _ClassVar[int]
    ROWS_READ_FIELD_NUMBER: _ClassVar[int]
    ROWS_DISCARDED_FIELD_NUMBER: _ClassVar[int]
    RUNS_WITH_UNCOUNTED_ROWS_FIELD_NUMBER: _ClassVar[int]
    runs: int
    runs_completed: int
    runs_canceled: int
    rows_read: int
    rows_discarded: int
    runs_with_uncounted_rows: int
    def __init__(self, runs: _Optional[int] = ..., runs_completed: _Optional[int] = ..., runs_canceled: _Optional[int] = ..., rows_read: _Optional[int] = ..., rows_discarded: _Optional[int] = ..., runs_with_uncounted_rows: _Optional[int] = ...) -> None: ...

class UsageDay(_message.Message):
    __slots__ = ("day", "rows_read", "runs")
    DAY_FIELD_NUMBER: _ClassVar[int]
    ROWS_READ_FIELD_NUMBER: _ClassVar[int]
    RUNS_FIELD_NUMBER: _ClassVar[int]
    day: _metrics_pb2.Date
    rows_read: int
    runs: int
    def __init__(self, day: _Optional[_Union[_metrics_pb2.Date, _Mapping]] = ..., rows_read: _Optional[int] = ..., runs: _Optional[int] = ...) -> None: ...

class JobUsage(_message.Message):
    __slots__ = ("job_id", "job_name", "kind", "totals", "duration_median_seconds")
    JOB_ID_FIELD_NUMBER: _ClassVar[int]
    JOB_NAME_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    TOTALS_FIELD_NUMBER: _ClassVar[int]
    DURATION_MEDIAN_SECONDS_FIELD_NUMBER: _ClassVar[int]
    job_id: str
    job_name: str
    kind: JobKind
    totals: UsageTotals
    duration_median_seconds: int
    def __init__(self, job_id: _Optional[str] = ..., job_name: _Optional[str] = ..., kind: _Optional[_Union[JobKind, str]] = ..., totals: _Optional[_Union[UsageTotals, _Mapping]] = ..., duration_median_seconds: _Optional[int] = ...) -> None: ...

class UsageErrorCount(_message.Message):
    __slots__ = ("category", "runs")
    CATEGORY_FIELD_NUMBER: _ClassVar[int]
    RUNS_FIELD_NUMBER: _ClassVar[int]
    category: RunErrorCategory
    runs: int
    def __init__(self, category: _Optional[_Union[RunErrorCategory, str]] = ..., runs: _Optional[int] = ...) -> None: ...

class GateRefusalCount(_message.Message):
    __slots__ = ("gate", "refusals")
    GATE_FIELD_NUMBER: _ClassVar[int]
    REFUSALS_FIELD_NUMBER: _ClassVar[int]
    gate: str
    refusals: int
    def __init__(self, gate: _Optional[str] = ..., refusals: _Optional[int] = ...) -> None: ...

class RunUsage(_message.Message):
    __slots__ = ("run_id", "status", "started_at", "ended_at", "rows_read", "tables_uncounted", "error_category", "error_step")
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    ENDED_AT_FIELD_NUMBER: _ClassVar[int]
    ROWS_READ_FIELD_NUMBER: _ClassVar[int]
    TABLES_UNCOUNTED_FIELD_NUMBER: _ClassVar[int]
    ERROR_CATEGORY_FIELD_NUMBER: _ClassVar[int]
    ERROR_STEP_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    status: _job_pb2.JobRunStatus
    started_at: _timestamp_pb2.Timestamp
    ended_at: _timestamp_pb2.Timestamp
    rows_read: int
    tables_uncounted: int
    error_category: RunErrorCategory
    error_step: RunErrorStep
    def __init__(self, run_id: _Optional[str] = ..., status: _Optional[_Union[_job_pb2.JobRunStatus, str]] = ..., started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., ended_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., rows_read: _Optional[int] = ..., tables_uncounted: _Optional[int] = ..., error_category: _Optional[_Union[RunErrorCategory, str]] = ..., error_step: _Optional[_Union[RunErrorStep, str]] = ...) -> None: ...

class GetAccountUsageRequest(_message.Message):
    __slots__ = ("account_id", "from_day", "to_day", "time_zone")
    ACCOUNT_ID_FIELD_NUMBER: _ClassVar[int]
    FROM_DAY_FIELD_NUMBER: _ClassVar[int]
    TO_DAY_FIELD_NUMBER: _ClassVar[int]
    TIME_ZONE_FIELD_NUMBER: _ClassVar[int]
    account_id: str
    from_day: _metrics_pb2.Date
    to_day: _metrics_pb2.Date
    time_zone: str
    def __init__(self, account_id: _Optional[str] = ..., from_day: _Optional[_Union[_metrics_pb2.Date, _Mapping]] = ..., to_day: _Optional[_Union[_metrics_pb2.Date, _Mapping]] = ..., time_zone: _Optional[str] = ...) -> None: ...

class GetAccountUsageResponse(_message.Message):
    __slots__ = ("totals", "duration_total_seconds", "days", "jobs", "errors", "refusals", "time_zone")
    TOTALS_FIELD_NUMBER: _ClassVar[int]
    DURATION_TOTAL_SECONDS_FIELD_NUMBER: _ClassVar[int]
    DAYS_FIELD_NUMBER: _ClassVar[int]
    JOBS_FIELD_NUMBER: _ClassVar[int]
    ERRORS_FIELD_NUMBER: _ClassVar[int]
    REFUSALS_FIELD_NUMBER: _ClassVar[int]
    TIME_ZONE_FIELD_NUMBER: _ClassVar[int]
    totals: UsageTotals
    duration_total_seconds: int
    days: _containers.RepeatedCompositeFieldContainer[UsageDay]
    jobs: _containers.RepeatedCompositeFieldContainer[JobUsage]
    errors: _containers.RepeatedCompositeFieldContainer[UsageErrorCount]
    refusals: _containers.RepeatedCompositeFieldContainer[GateRefusalCount]
    time_zone: str
    def __init__(self, totals: _Optional[_Union[UsageTotals, _Mapping]] = ..., duration_total_seconds: _Optional[int] = ..., days: _Optional[_Iterable[_Union[UsageDay, _Mapping]]] = ..., jobs: _Optional[_Iterable[_Union[JobUsage, _Mapping]]] = ..., errors: _Optional[_Iterable[_Union[UsageErrorCount, _Mapping]]] = ..., refusals: _Optional[_Iterable[_Union[GateRefusalCount, _Mapping]]] = ..., time_zone: _Optional[str] = ...) -> None: ...

class GetJobUsageRequest(_message.Message):
    __slots__ = ("account_id", "job_id", "from_day", "to_day", "time_zone")
    ACCOUNT_ID_FIELD_NUMBER: _ClassVar[int]
    JOB_ID_FIELD_NUMBER: _ClassVar[int]
    FROM_DAY_FIELD_NUMBER: _ClassVar[int]
    TO_DAY_FIELD_NUMBER: _ClassVar[int]
    TIME_ZONE_FIELD_NUMBER: _ClassVar[int]
    account_id: str
    job_id: str
    from_day: _metrics_pb2.Date
    to_day: _metrics_pb2.Date
    time_zone: str
    def __init__(self, account_id: _Optional[str] = ..., job_id: _Optional[str] = ..., from_day: _Optional[_Union[_metrics_pb2.Date, _Mapping]] = ..., to_day: _Optional[_Union[_metrics_pb2.Date, _Mapping]] = ..., time_zone: _Optional[str] = ...) -> None: ...

class GetJobUsageResponse(_message.Message):
    __slots__ = ("kind", "totals", "duration_median_seconds", "days", "runs", "time_zone")
    KIND_FIELD_NUMBER: _ClassVar[int]
    TOTALS_FIELD_NUMBER: _ClassVar[int]
    DURATION_MEDIAN_SECONDS_FIELD_NUMBER: _ClassVar[int]
    DAYS_FIELD_NUMBER: _ClassVar[int]
    RUNS_FIELD_NUMBER: _ClassVar[int]
    TIME_ZONE_FIELD_NUMBER: _ClassVar[int]
    kind: JobKind
    totals: UsageTotals
    duration_median_seconds: int
    days: _containers.RepeatedCompositeFieldContainer[UsageDay]
    runs: _containers.RepeatedCompositeFieldContainer[RunUsage]
    time_zone: str
    def __init__(self, kind: _Optional[_Union[JobKind, str]] = ..., totals: _Optional[_Union[UsageTotals, _Mapping]] = ..., duration_median_seconds: _Optional[int] = ..., days: _Optional[_Iterable[_Union[UsageDay, _Mapping]]] = ..., runs: _Optional[_Iterable[_Union[RunUsage, _Mapping]]] = ..., time_zone: _Optional[str] = ...) -> None: ...
