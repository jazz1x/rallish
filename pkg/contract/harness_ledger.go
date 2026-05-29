package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

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
	// LedgerEventActionDenied records that a pre-execution policy check denied or
	// flagged a pending action (G6 action-gate). It is a DECISION RECORD only:
	// rallish declares the policy and audits the decision; the runtime hook or
	// sandbox enforces. The entry never implies rallish executed or blocked a
	// command itself.
	LedgerEventActionDenied LedgerEventType = "action_denied"
	// LedgerEventSecretFlagged records that a secret-containment check flagged a
	// pending action as touching a sensitive path or carrying an over-broad token
	// (G6 containment). Like LedgerEventActionDenied it is a DECISION RECORD only:
	// rallish declares the policy and audits it; the runtime hook / sandbox
	// enforces. The entry never implies rallish read, redacted, or blocked the
	// secret itself.
	LedgerEventSecretFlagged LedgerEventType = "secret_flagged"
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
	// PrevHash links this entry to the previous one in the append-only ledger:
	// it is the previous entry's Hash, or LedgerGenesisHash for the first entry.
	// Part of the G4 tamper-evidence layer (linear hash chain, IETF AAT fields,
	// step 1 toward a CT-style Merkle log). Additive: omitted on un-chained entries.
	PrevHash string `json:"prev_hash,omitempty"`
	// Hash is the SHA-256 over the canonical form of this entry (with Hash zeroed)
	// concatenated with PrevHash; see ChainHash. It is EXCLUDED from its own
	// canonical input to avoid circularity. Additive: omitted on un-chained entries.
	Hash string `json:"hash,omitempty"`
}

// LedgerGenesisHash is the PrevHash of the first entry in a chain: 64 hex zeros
// (the all-zero SHA-256), a fixed genesis constant so the first link is well
// defined and the chain has no special-case in the hashed content.
const LedgerGenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// ChainHash is the SSOT hash function for the ledger tamper-evidence chain,
// shared by the single writer and any pure verifier so they cannot drift.
//
// It returns SHA-256( canonical(entry, Hash zeroed) || prevHash ), hex-encoded.
// The entry's own Hash is excluded from the hashed input (else it is circular);
// PrevHash IS part of the canonical content AND is appended once more per the
// chain formula, binding each entry to its predecessor. SchemaVersion is part of
// the canonical content, so a shape change is detectable.
//
// Canonical form (determinism, no JCS dependency): HarnessLedgerEntry is a fixed
// Go struct containing no maps, so encoding/json marshals its fields in a fixed
// declaration order with no insignificant whitespace — already deterministic and
// field-order independent. RFC 8785 JCS is only required once entries are
// re-serialized from untyped maps, which is out of scope for step 1.
func ChainHash(entry HarnessLedgerEntry, prevHash string) (string, error) {
	entry.Hash = ""
	canonical, err := json.Marshal(entry)
	if err != nil {
		return "", fmt.Errorf("canonicalize ledger entry: %w", err)
	}
	var b strings.Builder
	b.Grow(len(canonical) + len(prevHash))
	b.Write(canonical)
	b.WriteString(prevHash)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:]), nil
}

// NoBrokenLink is the brokenIndex VerifyChain returns when the chain is intact.
const NoBrokenLink = -1

// VerifyChain is the pure reader for the ledger tamper-evidence chain (G4): the
// query/audit layer over an already-loaded slice, no I/O. It walks the entries in
// order and, using the same ChainHash SSOT as the writer, checks two invariants
// per entry: (1) the recorded Hash equals a recomputation over the entry's
// canonical content (content tamper, e.g. a mutated Summary, breaks this), and
// (2) PrevHash links to the predecessor's Hash (LedgerGenesisHash for the first
// entry).
//
// It returns the index of the FIRST entry that violates either invariant and
// ok=false; an intact (or empty) chain returns (NoBrokenLink, true).
func VerifyChain(entries []HarnessLedgerEntry) (brokenIndex int, ok bool) {
	prev := LedgerGenesisHash
	for i, entry := range entries {
		if entry.PrevHash != prev {
			return i, false
		}
		want, err := ChainHash(entry, entry.PrevHash)
		if err != nil || entry.Hash != want {
			return i, false
		}
		prev = entry.Hash
	}
	return NoBrokenLink, true
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
