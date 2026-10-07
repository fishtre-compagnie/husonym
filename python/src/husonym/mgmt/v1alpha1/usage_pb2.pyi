import datetime

from buf.validate import validate_pb2 as _validate_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from mgmt.v1alpha1 import permission_pb2 as _permission_pb2
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class RunOutcome(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    RUN_OUTCOME_UNSPECIFIED: _ClassVar[RunOutcome]
    RUN_OUTCOME_COMPLETED: _ClassVar[RunOutcome]
    RUN_OUTCOME_FAILED: _ClassVar[RunOutcome]
    RUN_OUTCOME_CANCELED: _ClassVar[RunOutcome]
RUN_OUTCOME_UNSPECIFIED: RunOutcome
RUN_OUTCOME_COMPLETED: RunOutcome
RUN_OUTCOME_FAILED: RunOutcome
RUN_OUTCOME_CANCELED: RunOutcome

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
