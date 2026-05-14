package contract

import (
	"encoding/json"
	"testing"
)

func TestTaskStateIsTerminal(t *testing.T) {
	cases := []struct {
		state    TaskState
		terminal bool
	}{
		{TaskStateSubmitted, false},
		{TaskStateWorking, false},
		{TaskStateInputRequired, false},
		{TaskStateCompleted, true},
		{TaskStateFailed, true},
		{TaskStateCanceled, true},
		{TaskStateRejected, true},
	}
	for _, c := range cases {
		if got := c.state.IsTerminal(); got != c.terminal {
			t.Errorf("%s.IsTerminal() = %v, want %v", c.state, got, c.terminal)
		}
	}
}

func TestAgentCardJSONRoundTrip(t *testing.T) {
	card := AgentCard{
		Name:        "rallish",
		Description: "test",
		Version:     "0.1.0",
		Capabilities: AgentCapability{
			Streaming: true,
		},
		Skills: []AgentSkill{
			{ID: "test", Name: "Test"},
		},
	}
	b, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded AgentCard
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Name != card.Name {
		t.Errorf("name = %q, want %q", decoded.Name, card.Name)
	}
	if !decoded.Capabilities.Streaming {
		t.Error("expected streaming capability")
	}
	if len(decoded.Skills) != 1 || decoded.Skills[0].ID != "test" {
		t.Error("skills mismatch")
	}
}

func TestJSONRPCRequestRoundTrip(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "abc",
		Method:  "tasks/send",
		Params: map[string]any{
			"message": map[string]any{"parts": []any{map[string]any{"text": "hi"}}},
		},
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded JSONRPCRequest
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Method != req.Method {
		t.Errorf("method = %q, want %q", decoded.Method, req.Method)
	}
}

func TestJSONRPCResponseWithError(t *testing.T) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      1,
		Error: &JSONRPCError{
			Code:    -32700,
			Message: "Parse error",
		},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded JSONRPCResponse
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Error == nil {
		t.Fatal("expected error")
	}
	if decoded.Error.Code != -32700 {
		t.Errorf("code = %d, want -32700", decoded.Error.Code)
	}
}
