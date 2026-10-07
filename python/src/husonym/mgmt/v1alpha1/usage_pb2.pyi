import datetime

from buf.validate import validate_pb2 as _validate_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
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
RUN_OUTCOME_UNSPECIFIED: RunOutcome
RUN_OUTCOME_COMPLETED: RunOutcome
RUN_OUTCOME_FAILED: RunOutcome
RUN_OUTCOME_CANCELED: RunOutcome
USAGE_REPORTING_MODE_UNSPECIFIED: UsageReportingMode
USAGE_REPORTING_MODE_ONLINE: UsageReportingMode
USAGE_REPORTING_MODE_OFFLINE_REPORT: UsageReportingMode
USAGE_REPORTING_MODE_NONE: UsageReportingMode
USAGE_REPORT_STATUS_UNSPECIFIED: UsageReportStatus
USAGE_REPORT_STATUS_KEPT: UsageReportStatus
USAGE_REPORT_STATUS_TO_BE_SENT: UsageReportStatus
USAGE_REPORT_STATUS_SENT: UsageReportStatus
USAGE_REPORT_STATUS_NOT_SENT: UsageReportStatus

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
    __slots__ = ("job_id", "run_id", "started_at", "ended_at", "outcome", "rows_read", "rows_discarded", "retries", "tables_uncounted", "source_version_major")
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
    def __init__(self, job_id: _Optional[str] = ..., run_id: _Optional[str] = ..., started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., ended_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., outcome: _Optional[_Union[RunOutcome, str]] = ..., rows_read: _Optional[int] = ..., rows_discarded: _Optional[int] = ..., retries: _Optional[int] = ..., tables_uncounted: _Optional[int] = ..., source_version_major: _Optional[str] = ...) -> None: ...

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
