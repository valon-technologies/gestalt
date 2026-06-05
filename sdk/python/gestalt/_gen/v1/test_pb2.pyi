from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class HelloWorldRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class HelloWorldResponse(_message.Message):
    __slots__ = ()
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    message: str
    def __init__(self, message: _Optional[str] = ...) -> None: ...
