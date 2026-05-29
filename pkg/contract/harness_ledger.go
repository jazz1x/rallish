package contract

// LedgerEventType categorises append-only harness events.
type LedgerEventType string

// Harness ledger event types.
const (
	LedgerEventCycleCreated    LedgerEventType = "cycle_created"
	LedgerEventAgentTurn       LedgerEventType = "agent_turn"
	LedgerEventGatePassed      LedgerEventType = "gate_passed"
	LedgerEventGateFailed      LedgerEventType = "gate_failed"
	LedgerEventHandoffCreated  LedgerEventType = "handoff_created"
	LedgerEventCycleHalted     LedgerEventType = "cycle_halted"
	LedgerEventCycleCompleted  LedgerEventType = "cycle_completed"
	LedgerEventValidationGreen LedgerEventType = "validation_green"
)

// LedgerSchemaVersion is stamped on every ledger entry so the append-only JSONL
// shape is an explicit, versioned contract (Hyrum's law): readers can detect and
// migrate a shape change instead of breaking silently.
const LedgerSchemaVersion = "1"

// HarnessLedgerEntry is an append-only audit record for autonomous work.
type HarnessLedgerEntry struct {
	// SchemaVersion is the ledger entry schema version, always set, so the
	// append-only JSONL shape is an explicit contract for downstream readers.
	SchemaVersion string `json:"schema_version"`
	// At is the Unix millisecond timestamp for this event.
	At int64 `json:"at"`
	// CycleID identifies the cycle this event belongs to.
	CycleID string `json:"cycle_id"`
	// Type categorises the event.
	Type LedgerEventType `json:"type"`
	// Agent is the optional runtime or role responsible for the event.
	Agent string `json:"agent,omitempty"`
	// HandoffTo optionally names the next agent or role requested by the turn.
	HandoffTo string `json:"handoff_to,omitempty"`
	// Gate is the optional gate name for validation events.
	Gate string `json:"gate,omitempty"`
	// Summary is a compact human-readable event summary.
	Summary string `json:"summary,omitempty"`
	// Files lists touched or relevant files.
	Files []string `json:"files,omitempty"`
	// WorkContract carries the optional harness contract snapshot.
	WorkContract *WorkContract `json:"work_contract,omitempty"`
}

// NewHarnessLedgerEntry builds an audit entry with copied file metadata.
func NewHarnessLedgerEntry(at int64, cycleID string, typ LedgerEventType, summary string, files []string) HarnessLedgerEntry {
	return HarnessLedgerEntry{
		SchemaVersion: LedgerSchemaVersion,
		At:            at,
		CycleID:       cycleID,
		Type:          typ,
		Summary:       summary,
		Files:         append([]string(nil), files...),
	}
}

// NewGateLedgerEntry projects a gate report into an append-only ledger event.
func NewGateLedgerEntry(at int64, cycleID string, report GateReport) HarnessLedgerEntry {
	eventType := LedgerEventGateFailed
	if report.Passed {
		eventType = LedgerEventGatePassed
	}
	entry := NewHarnessLedgerEntry(at, cycleID, eventType, report.Stderr, nil)
	entry.Gate = report.Gate
	if report.Passed {
		entry.Summary = report.Stdout
	}
	return entry
}

// NewAgentTurnLedgerEntry projects a completed agent turn into the ledger.
func NewAgentTurnLedgerEntry(at int64, cycleID, agent string, resp TurnResponse) HarnessLedgerEntry {
	entry := NewHarnessLedgerEntry(at, cycleID, LedgerEventAgentTurn, resp.Summary, resp.Artifacts)
	entry.Agent = agent
	return entry
}

// NewHandoffLedgerEntry projects a turn response handoff into the ledger.
func NewHandoffLedgerEntry(at int64, cycleID, agent string, resp TurnResponse) HarnessLedgerEntry {
	entry := NewHarnessLedgerEntry(at, cycleID, LedgerEventHandoffCreated, resp.Summary, resp.Artifacts)
	entry.Agent = agent
	entry.HandoffTo = resp.HandoffTo
	return entry
}
