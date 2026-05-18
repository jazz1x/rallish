// Package contract defines the public wire types for rallish.
//
// This file contains the rally-mode baton-passing types used by the broker
// and CLI for interactive multi-participant sessions.
package contract

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// participantNameRe is the validation regex for participant names.
// It is an implementation detail of NewParticipantName; callers should use
// the constructor rather than this regex directly.
var participantNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,16}$`)

// sessionIDRe validates the rly_<unixms>_<rand4hex> session-id format.
var sessionIDRe = regexp.MustCompile(`^rly_[0-9]+_[0-9a-f]{4}$`)

// ParticipantName is a validated rally participant name.
// Construct via NewParticipantName; the zero value is invalid.
type ParticipantName string

// NewParticipantName returns a validated ParticipantName.
// Returns ErrInvalidName if s does not match ^[a-zA-Z0-9_-]{1,16}$.
func NewParticipantName(s string) (ParticipantName, error) {
	if !participantNameRe.MatchString(s) {
		return "", fmt.Errorf("participant %q: %w", s, ErrInvalidName)
	}
	return ParticipantName(s), nil
}

// String renders the name back to its plain string form.
func (p ParticipantName) String() string { return string(p) }

// MarshalJSON encodes ParticipantName as a plain JSON string.
func (p ParticipantName) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(p))
}

// UnmarshalJSON decodes a JSON string into a ParticipantName, validating the
// format via NewParticipantName. Returns an error wrapping ErrInvalidName if
// the string fails validation.
func (p *ParticipantName) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	np, err := NewParticipantName(s)
	if err != nil {
		return err
	}
	*p = np
	return nil
}

// SessionID is a validated rally session id of the form rly_<unixms>_<rand4hex>.
// Construct via NewSessionID; the zero value is invalid.
type SessionID string

// NewSessionID returns a validated SessionID.
// Returns ErrInvalidSessionID if s does not match rly_<unixms>_<rand4hex>.
func NewSessionID(s string) (SessionID, error) {
	if !sessionIDRe.MatchString(s) {
		return "", fmt.Errorf("session id %q: %w", s, ErrInvalidSessionID)
	}
	return SessionID(s), nil
}

// String renders the session id back to its plain string form.
func (id SessionID) String() string { return string(id) }

// MarshalJSON encodes SessionID as a plain JSON string.
func (id SessionID) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(id))
}

// UnmarshalJSON decodes a JSON string into a SessionID, validating the format
// via NewSessionID. Returns an error wrapping ErrInvalidSessionID on failure.
func (id *SessionID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	nid, err := NewSessionID(s)
	if err != nil {
		return err
	}
	*id = nid
	return nil
}

// Pattern is one of the rally-pattern conventions encoded in rally task strings
// as [pattern:<name>].
type Pattern int

const (
	// PatternUnknown indicates an unrecognised [pattern:…] tag.
	PatternUnknown Pattern = iota
	// PatternFreeform is the default pattern when no [pattern:…] tag is present.
	PatternFreeform
	// PatternCycle is the plan/execute/review pattern.
	PatternCycle
	// PatternDiscuss is the multi-perspective discussion pattern.
	PatternDiscuss
	// PatternHelp is the short stuck-help pattern.
	PatternHelp
)

// String returns the canonical lowercase name for the pattern.
func (p Pattern) String() string {
	switch p {
	case PatternFreeform:
		return "freeform"
	case PatternCycle:
		return "cycle"
	case PatternDiscuss:
		return "discuss"
	case PatternHelp:
		return "help"
	default:
		return "unknown"
	}
}

// ParsePattern parses an optional [pattern:<name>] prefix from a task string.
//
//   - "[pattern:cycle] rest" → (PatternCycle, "rest", nil)
//   - "no prefix here"       → (PatternFreeform, "no prefix here", nil)
//   - "[pattern:bad] …"      → (PatternUnknown, "", ErrInvalidPattern)
func ParsePattern(taskPrefix string) (Pattern, string, error) {
	if !strings.HasPrefix(taskPrefix, "[pattern:") {
		return PatternFreeform, taskPrefix, nil
	}
	end := strings.Index(taskPrefix, "]")
	if end < 0 {
		return PatternUnknown, "", fmt.Errorf("unclosed [pattern: tag: %w", ErrInvalidPattern)
	}
	name := taskPrefix[len("[pattern:"):end]
	rest := strings.TrimLeft(taskPrefix[end+1:], " ")

	switch name {
	case "freeform":
		return PatternFreeform, rest, nil
	case "cycle":
		return PatternCycle, rest, nil
	case "discuss":
		return PatternDiscuss, rest, nil
	case "help":
		return PatternHelp, rest, nil
	default:
		return PatternUnknown, "", fmt.Errorf("pattern %q: %w", name, ErrInvalidPattern)
	}
}

// RallyState is the current lifecycle state of a rally session.
type RallyState string

// Rally session states.
const (
	// RallyStateIdle means the session is created but no participant holds the baton.
	RallyStateIdle RallyState = "idle"
	// RallyStateCompleted means all participants have passed the baton and the session is done.
	RallyStateCompleted RallyState = "completed"
	// RallyStateInterrupted means the session was forcibly ended (e.g. broker SIGTERM).
	RallyStateInterrupted RallyState = "interrupted"
)

// RallyTurnState returns the state string for a named participant holding the baton.
// e.g. "alice_turn".
func RallyTurnState(name string) RallyState {
	return RallyState(name + "_turn")
}

// BatonHandoff records a single baton transfer in the session history.
type BatonHandoff struct {
	// From is the participant who passed the baton.
	From string `json:"from"`
	// To is the participant who received the baton.
	To string `json:"to"`
	// TurnN is the turn number at which the handoff occurred.
	TurnN int `json:"turn_n"`
	// Note is an optional human-readable message from the passing participant.
	Note string `json:"note,omitempty"`
	// At is the Unix millisecond timestamp of the handoff.
	At int64 `json:"at"`
}

// BatonEvent is the SSE payload emitted to a participant when they receive the baton.
type BatonEvent struct {
	// TurnN is the current turn number.
	TurnN int `json:"turn_n"`
	// From is the participant who passed the baton (empty on first turn).
	From string `json:"from,omitempty"`
	// Note is the optional message passed along with the baton.
	Note string `json:"note,omitempty"`
	// Closed, when true, signals that the session has been interrupted and the
	// SSE stream is being terminated by the broker (e.g. on SIGTERM).
	Closed bool `json:"closed,omitempty"`
}

// DoneRequest is the body for POST /rally/sessions/:id/done.
type DoneRequest struct {
	// Note is an optional message to pass to the next participant.
	Note string `json:"note,omitempty"`
	// HandoffTo optionally names a specific participant to receive the baton next.
	// If empty, the broker uses round-robin ordering.
	HandoffTo string `json:"handoff_to,omitempty"`
}

// NewRallyRequest is the body for POST /rally/sessions.
type NewRallyRequest struct {
	// Participants is the ordered list of participant names.
	Participants []string `json:"participants"`
	// Repo is the optional repository path for context.
	Repo string `json:"repo,omitempty"`
	// Task is the optional task description.
	Task string `json:"task,omitempty"`
	// FirstHolder optionally pre-assigns the baton to this participant at session-create time.
	// Must be one of the Participants. When set, the session starts in <first_holder>_turn
	// state with TurnN=1 rather than idle/0.
	FirstHolder string `json:"first_holder,omitempty"`
}

// RallySession is the full state of a rally session as returned by GET /rally/sessions/:id.
type RallySession struct {
	// ID is the unique session identifier (format: rly_<unixmillis>_<rand4hex>).
	ID string `json:"id"`
	// Participants is the ordered list of participant names.
	Participants []string `json:"participants"`
	// Repo is the optional repository path.
	Repo string `json:"repo,omitempty"`
	// Task is the optional task description.
	Task string `json:"task,omitempty"`
	// Holder is the name of the current baton holder (empty when idle).
	Holder string `json:"holder"`
	// TurnN is the current turn number (0 = not started).
	TurnN int `json:"turn_n"`
	// Status is the current lifecycle state of the session.
	Status RallyState `json:"status"`
	// LastSeen maps each participant name to their last heartbeat Unix millisecond timestamp.
	LastSeen map[string]int64 `json:"last_seen"`
	// History is the ordered list of baton handoffs that have occurred.
	History []BatonHandoff `json:"history"`
	// CreatedAt is the Unix millisecond timestamp when the session was created.
	CreatedAt int64 `json:"created_at"`
}
