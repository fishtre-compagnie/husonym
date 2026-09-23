import datetime

from buf.validate import validate_pb2 as _validate_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from mgmt.v1alpha1 import secret_pb2 as _secret_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class SettingCheckLevel(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SETTING_CHECK_LEVEL_UNSPECIFIED: _ClassVar[SettingCheckLevel]
    SETTING_CHECK_LEVEL_BLOCKING: _ClassVar[SettingCheckLevel]
    SETTING_CHECK_LEVEL_WARNING: _ClassVar[SettingCheckLevel]
    SETTING_CHECK_LEVEL_INFO: _ClassVar[SettingCheckLevel]
SETTING_CHECK_LEVEL_UNSPECIFIED: SettingCheckLevel
SETTING_CHECK_LEVEL_BLOCKING: SettingCheckLevel
SETTING_CHECK_LEVEL_WARNING: SettingCheckLevel
SETTING_CHECK_LEVEL_INFO: SettingCheckLevel

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
    __slots__ = ("anonymization_consistency", "oidc_provider")
    ANONYMIZATION_CONSISTENCY_FIELD_NUMBER: _ClassVar[int]
    OIDC_PROVIDER_FIELD_NUMBER: _ClassVar[int]
    anonymization_consistency: AnonymizationConsistency
    oidc_provider: OidcProvider
    def __init__(self, anonymization_consistency: _Optional[_Union[AnonymizationConsistency, _Mapping]] = ..., oidc_provider: _Optional[_Union[OidcProvider, _Mapping]] = ...) -> None: ...

class AnonymizationConsistency(_message.Message):
    __slots__ = ("derivation_key",)
    DERIVATION_KEY_FIELD_NUMBER: _ClassVar[int]
    derivation_key: str
    def __init__(self, derivation_key: _Optional[str] = ...) -> None: ...

class OidcProvider(_message.Message):
    __slots__ = ("issuer", "client_id", "client_secret")
    ISSUER_FIELD_NUMBER: _ClassVar[int]
    CLIENT_ID_FIELD_NUMBER: _ClassVar[int]
    CLIENT_SECRET_FIELD_NUMBER: _ClassVar[int]
    issuer: str
    client_id: str
    client_secret: str
    def __init__(self, issuer: _Optional[str] = ..., client_id: _Optional[str] = ..., client_secret: _Optional[str] = ...) -> None: ...

class SettingCheck(_message.Message):
    __slots__ = ("check", "level", "detail", "remedy")
    CHECK_FIELD_NUMBER: _ClassVar[int]
    LEVEL_FIELD_NUMBER: _ClassVar[int]
    DETAIL_FIELD_NUMBER: _ClassVar[int]
    REMEDY_FIELD_NUMBER: _ClassVar[int]
    check: str
    level: SettingCheckLevel
    detail: str
    remedy: str
    def __init__(self, check: _Optional[str] = ..., level: _Optional[_Union[SettingCheckLevel, str]] = ..., detail: _Optional[str] = ..., remedy: _Optional[str] = ...) -> None: ...

class TestAccountSettingRequest(_message.Message):
    __slots__ = ("account_id", "config")
    ACCOUNT_ID_FIELD_NUMBER: _ClassVar[int]
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    account_id: str
    config: AccountSettingConfig
    def __init__(self, account_id: _Optional[str] = ..., config: _Optional[_Union[AccountSettingConfig, _Mapping]] = ...) -> None: ...

class TestAccountSettingResponse(_message.Message):
    __slots__ = ("checks", "ok")
    CHECKS_FIELD_NUMBER: _ClassVar[int]
    OK_FIELD_NUMBER: _ClassVar[int]
    checks: _containers.RepeatedCompositeFieldContainer[SettingCheck]
    ok: bool
    def __init__(self, checks: _Optional[_Iterable[_Union[SettingCheck, _Mapping]]] = ..., ok: _Optional[bool] = ...) -> None: ...

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
