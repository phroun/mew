"""kittytk - the KittyTK display-protocol client, in Python.

A pure-protocol client: it links nothing of the rendering side and speaks
the identical wire language the Go client does, so a Python app drives the
same display host (kittytk-tui / kittytk-sdl) as any other.
"""

from .protocol import (
    ArgInfo,
    CallInfo,
    Event,
    EventInfo,
    FlagState,
    ParseError,
    PropInfo,
    TypeInfo,
    Value,
    ValueKind,
    Vocabulary,
    decode_vocabulary,
    parse,
    parse_event,
    quote,
    quote_blob,
)
from .client import (
    ASK_DARK,
    ASK_DESKTOP,
    Blob,
    Button,
    CACHE_MARK,
    Checkbox,
    Conn,
    HOST_STATE,
    Handle,
    Label,
    STORE_BLOB,
    STORE_DATA,
    STORE_DONE,
    STORE_ERROR,
    STORE_GONE,
    Selector,
    Store,
    TextInput,
    UI,
    Window,
    default_endpoint,
    default_socket_path,
    dial,
    dial_solo,
)

__all__ = [
    "Event", "FlagState", "ParseError", "Value", "ValueKind",
    "ArgInfo", "CallInfo", "EventInfo",
    "PropInfo", "TypeInfo", "Vocabulary", "decode_vocabulary",
    "parse", "parse_event", "quote", "quote_blob",
    "Button", "Checkbox", "Conn", "Handle", "Label", "Selector",
    "TextInput", "UI", "Window",
    "Blob", "Store", "CACHE_MARK",
    "STORE_BLOB", "STORE_DONE", "STORE_DATA", "STORE_GONE", "STORE_ERROR",
    "HOST_STATE", "ASK_DARK", "ASK_DESKTOP",
    "default_endpoint", "default_socket_path", "dial", "dial_solo",
]
