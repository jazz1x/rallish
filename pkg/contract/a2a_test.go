package contract

import (
	"encoding/json"
	"strings"
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
		ProtocolVersion: ProtocolVersion,
		Name:            "rallish",
		Description:     "test",
		Version:         "0.1.0",
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
	// The versioned-surface field must serialize under its wire name.
	if !strings.Contains(string(b), `"protocolVersion":"1.0"`) {
		t.Errorf("card JSON missing protocolVersion: %s", b)
	}
	var decoded AgentCard
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocolVersion = %q, want %q", decoded.ProtocolVersion, ProtocolVersion)
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
		Method:  MethodSendMessage,
		Params:  json.RawMessage(`{"message":{"parts":[{"text":"hi"}]}}`),
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
	// Params round-trip as raw bytes; decode into the typed param struct.
	var params SendMessageParams
	if err := json.Unmarshal(decoded.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if len(params.Message.Parts) != 1 || params.Message.Parts[0].Text != "hi" {
		t.Errorf("params message mismatch: %+v", params.Message)
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
