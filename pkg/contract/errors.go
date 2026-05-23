package contract

import "errors"

// ErrInvalidName is returned when a participant name does not match the allowed pattern.
var ErrInvalidName = errors.New("rally: invalid participant name")

// ErrInvalidSessionID is returned when a session id string does not match the
// rly_<unixms>_<rand4hex> format.
var ErrInvalidSessionID = errors.New("rally: invalid session id format")

// ErrInvalidPattern is returned when a task prefix contains a [pattern:…] tag
// whose name is not one of the recognised patterns.
var ErrInvalidPattern = errors.New("rally: invalid pattern in task prefix")

// ErrNotHolder is returned when an operation requires the caller to be the
// current baton holder but they are not.
var ErrNotHolder = errors.New("rally: not the current baton holder")

// ErrSessionNotFound is returned when a rally session id is not present in the store.
var ErrSessionNotFound = errors.New("rally: session not found")

// ErrDuplicateName is returned when the same participant name appears more than
// once in a NewRallyRequest.
var ErrDuplicateName = errors.New("rally: duplicate participant name")

// ErrTooFewParticipants is returned when fewer than two participants are supplied.
var ErrTooFewParticipants = errors.New("rally: at least two participants required")

// Cycle errors.
var (
	// ErrInvalidCyclePhase is returned when a cycle phase string is not recognised.
	ErrInvalidCyclePhase = errors.New("cycle: invalid phase")
	// ErrInvalidHaltReason is returned when a halt reason string is not recognised.
	ErrInvalidHaltReason = errors.New("cycle: invalid halt reason")
	// ErrCycleNotFound is returned when a cycle id is not present in the store.
	ErrCycleNotFound = errors.New("cycle: not found")
	// ErrCycleHalted is returned when an operation is requested on a halted cycle.
	ErrCycleHalted = errors.New("cycle: halted")
	// ErrMainBranchForbidden is returned when a cycle targets the main branch.
	ErrMainBranchForbidden = errors.New("cycle: main branch is forbidden")
	// ErrGoalRequired is returned when next_cycle_goal is empty.
	ErrGoalRequired = errors.New("cycle: next_cycle_goal is required")
)
