// Package contract defines the public wire types for rallish.
//
// This file contains A2A (Agent2Agent) protocol extensions layered on top of
// the internal turn-taking contract. All A2A structs follow the Linux-Foundation
// A2A Protocol v1.0 specification (https://a2a-protocol.org) where applicable.
package contract

import "encoding/json"

// ProtocolVersion is the A2A protocol version this broker conforms to.
// It is advertised in the AgentCard so clients can negotiate the wire shape
// (versioned public surface; Hyrum's law). Distinct from the agent's own
// build version carried in AgentCard.Version.
const ProtocolVersion = "1.0"

// A2A v1.0 JSON-RPC method names (PascalCase, per the Linux-Foundation spec).
// The legacy lowercase tasks/* names are retained as back-compat aliases in the
// broker dispatch but are NOT the conformance target.
const (
	MethodSendMessage     = "SendMessage"
	MethodGetTask         = "GetTask"
	MethodCancelTask      = "CancelTask"
	MethodSubscribeToTask = "SubscribeToTask"

	// Legacy draft method names (back-compat aliases only).
	LegacyMethodTasksSend          = "tasks/send"
	LegacyMethodTasksSendSubscribe = "tasks/sendSubscribe"
	LegacyMethodTasksGet           = "tasks/get"
	LegacyMethodTasksCancel        = "tasks/cancel"
)

// TaskState represents the A2A task lifecycle states.
type TaskState string

// A2A task states.
const (
	TaskStateSubmitted     TaskState = "submitted"
	TaskStateWorking       TaskState = "working"
	TaskStateInputRequired TaskState = "input_required"
	TaskStateCompleted     TaskState = "completed"
	TaskStateFailed        TaskState = "failed"
	TaskStateCanceled      TaskState = "canceled"
	TaskStateRejected      TaskState = "rejected"
)

// IsTerminal reports whether the task has reached a terminal state.
func (s TaskState) IsTerminal() bool {
	switch s {
	case TaskStateCompleted, TaskStateFailed, TaskStateCanceled, TaskStateRejected:
		return true
	}
	return false
}

// AgentCapability flags supported by the broker.
type AgentCapability struct {
	Streaming              bool `json:"streaming"`
	PushNotifications      bool `json:"pushNotifications"`
	StateTransitionHistory bool `json:"stateTransitionHistory"`
}

// AgentSkill describes a single capability advertised in the AgentCard.
type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Examples    []string `json:"examples,omitempty"`
}

// AgentCard is the well-known discovery document for A2A clients.
// Published at GET /.well-known/agent-card.json (A2A v1.0 path); the legacy
// GET /.well-known/agent.json path is served as a back-compat alias.
type AgentCard struct {
	// ProtocolVersion is the A2A protocol version the agent conforms to
	// (e.g. "1.0"). A v1.0 client uses this to negotiate the wire shape.
	ProtocolVersion    string          `json:"protocolVersion"`
	Name               string          `json:"name"`
	Description        string          `json:"description"`
	Version            string          `json:"version"`
	URL                string          `json:"url"`
	DocumentationURL   string          `json:"documentationUrl,omitempty"`
	Capabilities       AgentCapability `json:"capabilities"`
	Skills             []AgentSkill    `json:"skills"`
	DefaultInputModes  []string        `json:"defaultInputModes"`
	DefaultOutputModes []string        `json:"defaultOutputModes"`
	// Signature is an optional ed25519 signature over the canonical form of this
	// card (F16, verifiable interop). It is additive and EXCLUDED from its own
	// signed input to avoid circularity; see SignAgentCard / VerifyAgentCard. A
	// card with a nil Signature is an unsigned card (the broker default today).
	Signature *AgentCardSignature `json:"signature,omitempty"`
}

// AgentCardSignature is an embedded, self-certifying ed25519 signature over an
// AgentCard (F16 — a client can verify the card's authenticity). It carries the
// signer's public key so verification needs no out-of-band key fetch.
//
// KEY-MANAGEMENT BOUNDARY (deferred, per north-star §G3): this is a "signed card
// with a caller-supplied key; key provenance/distribution is deferred." There is
// deliberately NO secure key storage, rotation, CA, or PKI here — the caller
// supplies (or generates) the ed25519 key, and trust in the embedded public key
// is established out-of-band by the consumer. Mutual auth is also still deferred.
type AgentCardSignature struct {
	// Algorithm is the JOSE-style signature algorithm identifier. Only "EdDSA"
	// (ed25519) is produced/accepted today; an unknown alg fails verification.
	Algorithm string `json:"algorithm"`
	// PublicKey is the base64 (std, padded) ed25519 public key the signature
	// verifies against (32 bytes when decoded).
	PublicKey string `json:"publicKey"`
	// Signature is the base64 (std, padded) ed25519 signature over the canonical
	// form of the card with this Signature field excluded (non-circular).
	Signature string `json:"signature"`
}

// A2APart is a polymorphic message part (text or structured data).
//
// The discriminator field is "kind" per A2A v1.0 (Linux-Foundation spec).
// The legacy draft name "type" is no longer used on the wire; a client
// sending the legacy field will receive a parse error under DisallowUnknownFields
// (parse-don't-validate; RFC 9413 / Postel critique), which is intentional —
// v1.0 conformance is the target.
type A2APart struct {
	Kind string         `json:"kind"` // "text" | "data"
	Text string         `json:"text,omitempty"`
	Data map[string]any `json:"data,omitempty"`
}

// A2AMessage maps to an A2A Message within a Task.
type A2AMessage struct {
	Role  string    `json:"role"` // "user" | "agent"
	Parts []A2APart `json:"parts"`
}

// A2AArtifact maps to an A2A Artifact produced by a task.
type A2AArtifact struct {
	Name  string    `json:"name,omitempty"`
	Parts []A2APart `json:"parts"`
}

// A2ATask is the A2A representation of a rallish session.
type A2ATask struct {
	ID        string        `json:"id"`
	SessionID string        `json:"sessionId"`
	Status    A2ATaskStatus `json:"status"`
	Messages  []A2AMessage  `json:"messages,omitempty"`
	Artifacts []A2AArtifact `json:"artifacts,omitempty"`
}

// A2ATaskStatus carries the current state and optional reason.
type A2ATaskStatus struct {
	State  TaskState `json:"state"`
	Reason string    `json:"reason,omitempty"`
}

// A2ATaskUpdateEvent is streamed over SSE for long-running tasks.
type A2ATaskUpdateEvent struct {
	ID       string        `json:"id"`
	Status   A2ATaskStatus `json:"status"`
	Artifact *A2AArtifact  `json:"artifact,omitempty"`
	Final    bool          `json:"final,omitempty"`
}

// JSONRPCRequest is the envelope for A2A JSON-RPC 2.0 requests.
//
// Params is kept as a raw message so the dispatcher can decode it into the
// per-method typed param struct under DisallowUnknownFields (parse-don't-validate;
// strict, not liberal, at the interface — RFC 9413 / Postel critique). An
// unknown or extra field is an ERROR, never silently dropped.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// SendMessageParams is the typed param set for the SendMessage method.
type SendMessageParams struct {
	Message A2AMessage `json:"message"`
}

// TaskIDParams is the typed param set for GetTask / CancelTask / SubscribeToTask.
type TaskIDParams struct {
	ID string `json:"id"`
}

// JSONRPCResponse is the envelope for A2A JSON-RPC 2.0 responses.
type JSONRPCResponse struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id,omitempty"`
	Result  map[string]any `json:"result,omitempty"`
	Error   *JSONRPCError  `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}
