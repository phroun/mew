"""kittytk - the KittyTK display-protocol client, in Python.

A pure-protocol client: it links nothing of the rendering side and speaks
the identical wire language the Go client does, so a Python app drives the
same display host (kittytk-tui / kittytk-sdl) as any other.
"""

from .protocol import (
    Event,
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
)
from .client import (
    Button,
    Checkbox,
    Conn,
    Handle,
    Label,
    Selector,
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
    "PropInfo", "TypeInfo", "Vocabulary", "decode_vocabulary",
    "parse", "parse_event", "quote",
    "Button", "Checkbox", "Conn", "Handle", "Label", "Selector",
    "TextInput", "UI", "Window",
    "default_endpoint", "default_socket_path", "dial", "dial_solo",
]
