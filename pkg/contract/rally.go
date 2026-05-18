// Package contract defines the public wire types for rallish.
//
// This file contains the rally-mode baton-passing types used by the broker
// and CLI for interactive multi-participant sessions.
package contract

import "regexp"

// ParticipantNameRe is the regex every component uses to validate
// rally participant names.
var ParticipantNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,16}$`)

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
