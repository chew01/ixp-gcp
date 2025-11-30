from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class StartTelemetryRequest(_message.Message):
    __slots__ = ("kafka_broker_addr", "topic")
    KAFKA_BROKER_ADDR_FIELD_NUMBER: _ClassVar[int]
    TOPIC_FIELD_NUMBER: _ClassVar[int]
    kafka_broker_addr: str
    topic: str
    def __init__(self, kafka_broker_addr: _Optional[str] = ..., topic: _Optional[str] = ...) -> None: ...

class StartCallbackRequest(_message.Message):
    __slots__ = ("server_addr",)
    SERVER_ADDR_FIELD_NUMBER: _ClassVar[int]
    server_addr: str
    def __init__(self, server_addr: _Optional[str] = ...) -> None: ...

class ConfigureRequest(_message.Message):
    __slots__ = ("msg",)
    MSG_FIELD_NUMBER: _ClassVar[int]
    msg: str
    def __init__(self, msg: _Optional[str] = ...) -> None: ...

class StartResponse(_message.Message):
    __slots__ = ("status", "message")
    STATUS_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    status: str
    message: str
    def __init__(self, status: _Optional[str] = ..., message: _Optional[str] = ...) -> None: ...

class ConfigureResponse(_message.Message):
    __slots__ = ("status",)
    STATUS_FIELD_NUMBER: _ClassVar[int]
    status: str
    def __init__(self, status: _Optional[str] = ...) -> None: ...
