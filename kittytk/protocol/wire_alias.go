package protocol

// The wire language lives in its own package so a client can depend on
// it without depending on the toolkit: parsing, values, events, replies
// and the describe stream's shapes are all a client needs, and none of
// them know anything about trinkets or the registry.
//
// This file re-exports that package under the names the toolkit has
// always used, so host-side code keeps referring to protocol.Value,
// protocol.Parse and the rest. The registry, the session and property
// registration stay here, where they belong.

import "github.com/phroun/kittytk/wire"

// Language types.
type (
	Arg             = wire.Arg
	Statement       = wire.Statement
	Script          = wire.Script
	Value           = wire.Value
	ValueKind       = wire.ValueKind
	FlagState       = wire.FlagState
	Event           = wire.Event
	Reply           = wire.Reply
	Scanner         = wire.Scanner
	PropInfo        = wire.PropInfo
	TypeInfo        = wire.TypeInfo
	EventInfo       = wire.EventInfo
	EventFieldDesc  = wire.EventFieldDesc
	Vocabulary      = wire.Vocabulary
	EventDispatcher = wire.EventDispatcher
)

// Flag states and value kinds.
const (
	FlagNone          = wire.FlagNone
	FlagTrue          = wire.FlagTrue
	FlagFalse         = wire.FlagFalse
	FlagIndeterminate = wire.FlagIndeterminate

	WordValue   = wire.WordValue
	NumberValue = wire.NumberValue
	StringValue = wire.StringValue
	BlockValue  = wire.BlockValue
)

// Language functions.
var (
	Parse              = wire.Parse
	Quote              = wire.Quote
	NewEvent           = wire.NewEvent
	ParseEvent         = wire.ParseEvent
	EncodeReply        = wire.EncodeReply
	DecodeReply        = wire.DecodeReply
	EncodeError        = wire.EncodeError
	NewScanner         = wire.NewScanner
	DecodeVocabulary   = wire.DecodeVocabulary
	NewEventDispatcher = wire.NewEventDispatcher
)
