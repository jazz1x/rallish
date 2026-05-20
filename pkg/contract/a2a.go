// Package contract defines the public wire types for rallish.
//
// This file contains A2A (Agent2Agent) protocol extensions layered on top of
// the internal turn-taking contract. All A2A structs follow the Google A2A
// Protocol specification (https://github.com/a2aproject/A2A) where applicable.
package contract

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
// Published at GET /.well-known/agent.json
type AgentCard struct {
	Name               string          `json:"name"`
	Description        string          `json:"description"`
	Version            string          `json:"version"`
	URL                string          `json:"url"`
	DocumentationURL   string          `json:"documentationUrl,omitempty"`
	Capabilities       AgentCapability `json:"capabilities"`
	Skills             []AgentSkill    `json:"skills"`
	DefaultInputModes  []string        `json:"defaultInputModes"`
	DefaultOutputModes []string        `json:"defaultOutputModes"`
}

// A2APart is a polymorphic message part (text or structured data).
type A2APart struct {
	Type string         `json:"type"` // "text" | "data"
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
type JSONRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id,omitempty"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
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
