from google.protobuf import descriptor_pb2 as _descriptor_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Permission(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PERMISSION_UNSPECIFIED: _ClassVar[Permission]
    PERMISSION_ACCOUNT_VIEW: _ClassVar[Permission]
    PERMISSION_ACCOUNT_EDIT: _ClassVar[Permission]
    PERMISSION_ACCOUNT_CREATE: _ClassVar[Permission]
    PERMISSION_ACCOUNT_DELETE: _ClassVar[Permission]
    PERMISSION_CONNECTION_VIEW: _ClassVar[Permission]
    PERMISSION_CONNECTION_VIEW_SENSITIVE: _ClassVar[Permission]
    PERMISSION_CONNECTION_CREATE: _ClassVar[Permission]
    PERMISSION_CONNECTION_EDIT: _ClassVar[Permission]
    PERMISSION_CONNECTION_DELETE: _ClassVar[Permission]
    PERMISSION_JOB_VIEW: _ClassVar[Permission]
    PERMISSION_JOB_CREATE: _ClassVar[Permission]
    PERMISSION_JOB_EDIT: _ClassVar[Permission]
    PERMISSION_JOB_EXECUTE: _ClassVar[Permission]
    PERMISSION_JOB_DELETE: _ClassVar[Permission]
PERMISSION_UNSPECIFIED: Permission
PERMISSION_ACCOUNT_VIEW: Permission
PERMISSION_ACCOUNT_EDIT: Permission
PERMISSION_ACCOUNT_CREATE: Permission
PERMISSION_ACCOUNT_DELETE: Permission
PERMISSION_CONNECTION_VIEW: Permission
PERMISSION_CONNECTION_VIEW_SENSITIVE: Permission
PERMISSION_CONNECTION_CREATE: Permission
PERMISSION_CONNECTION_EDIT: Permission
PERMISSION_CONNECTION_DELETE: Permission
PERMISSION_JOB_VIEW: Permission
PERMISSION_JOB_CREATE: Permission
PERMISSION_JOB_EDIT: Permission
PERMISSION_JOB_EXECUTE: Permission
PERMISSION_JOB_DELETE: Permission
REQUIRES_FIELD_NUMBER: _ClassVar[int]
requires: _descriptor.FieldDescriptor

class ProcedurePermissions(_message.Message):
    __slots__ = ("all_of", "none")
    ALL_OF_FIELD_NUMBER: _ClassVar[int]
    NONE_FIELD_NUMBER: _ClassVar[int]
    all_of: _containers.RepeatedScalarFieldContainer[Permission]
    none: bool
    def __init__(self, all_of: _Optional[_Iterable[_Union[Permission, str]]] = ..., none: _Optional[bool] = ...) -> None: ...
