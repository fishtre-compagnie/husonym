import datetime

from buf.validate import validate_pb2 as _validate_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from mgmt.v1alpha1 import secret_pb2 as _secret_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class AccountSetting(_message.Message):
    __slots__ = ("account_id", "config", "secret_fingerprints", "created_at", "updated_at", "created_by_user_id", "updated_by_user_id")
    class SecretFingerprintsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ACCOUNT_ID_FIELD_NUMBER: _ClassVar[int]
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    SECRET_FINGERPRINTS_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_USER_ID_FIELD_NUMBER: _ClassVar[int]
    UPDATED_BY_USER_ID_FIELD_NUMBER: _ClassVar[int]
    account_id: str
    config: AccountSettingConfig
    secret_fingerprints: _containers.ScalarMap[str, str]
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    created_by_user_id: str
    updated_by_user_id: str
    def __init__(self, account_id: _Optional[str] = ..., config: _Optional[_Union[AccountSettingConfig, _Mapping]] = ..., secret_fingerprints: _Optional[_Mapping[str, str]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., created_by_user_id: _Optional[str] = ..., updated_by_user_id: _Optional[str] = ...) -> None: ...

class AccountSettingConfig(_message.Message):
    __slots__ = ("anonymization_consistency",)
    ANONYMIZATION_CONSISTENCY_FIELD_NUMBER: _ClassVar[int]
    anonymization_consistency: AnonymizationConsistency
    def __init__(self, anonymization_consistency: _Optional[_Union[AnonymizationConsistency, _Mapping]] = ...) -> None: ...

class AnonymizationConsistency(_message.Message):
    __slots__ = ("derivation_key",)
    DERIVATION_KEY_FIELD_NUMBER: _ClassVar[int]
    derivation_key: str
    def __init__(self, derivation_key: _Optional[str] = ...) -> None: ...

class GetAccountSettingsRequest(_message.Message):
    __slots__ = ("account_id",)
    ACCOUNT_ID_FIELD_NUMBER: _ClassVar[int]
    account_id: str
    def __init__(self, account_id: _Optional[str] = ...) -> None: ...

class GetAccountSettingsResponse(_message.Message):
    __slots__ = ("settings",)
    SETTINGS_FIELD_NUMBER: _ClassVar[int]
    settings: _containers.RepeatedCompositeFieldContainer[AccountSetting]
    def __init__(self, settings: _Optional[_Iterable[_Union[AccountSetting, _Mapping]]] = ...) -> None: ...

class SetAccountSettingRequest(_message.Message):
    __slots__ = ("account_id", "config")
    ACCOUNT_ID_FIELD_NUMBER: _ClassVar[int]
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    account_id: str
    config: AccountSettingConfig
    def __init__(self, account_id: _Optional[str] = ..., config: _Optional[_Union[AccountSettingConfig, _Mapping]] = ...) -> None: ...

class SetAccountSettingResponse(_message.Message):
    __slots__ = ("setting",)
    SETTING_FIELD_NUMBER: _ClassVar[int]
    setting: AccountSetting
    def __init__(self, setting: _Optional[_Union[AccountSetting, _Mapping]] = ...) -> None: ...

class GetAccountConsistencyKeyRequest(_message.Message):
    __slots__ = ("account_id", "generate_if_absent")
    ACCOUNT_ID_FIELD_NUMBER: _ClassVar[int]
    GENERATE_IF_ABSENT_FIELD_NUMBER: _ClassVar[int]
    account_id: str
    generate_if_absent: bool
    def __init__(self, account_id: _Optional[str] = ..., generate_if_absent: _Optional[bool] = ...) -> None: ...

class GetAccountConsistencyKeyResponse(_message.Message):
    __slots__ = ("key",)
    KEY_FIELD_NUMBER: _ClassVar[int]
    key: str
    def __init__(self, key: _Optional[str] = ...) -> None: ...
