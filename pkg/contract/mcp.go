// Package contract defines the public wire types for rallish.
//
// This file contains the MCP (Model Context Protocol) 2025-03-26 wire types
// used by the rally MCP server surface.
package contract

import "encoding/json"

// MCPProtocolVersion is the MCP protocol version advertised by the broker.
const MCPProtocolVersion = "2025-03-26"

// MCPJSONRPCRequest is a JSON-RPC 2.0 request used by the MCP transport.
type MCPJSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// MCPJSONRPCResponse is a JSON-RPC 2.0 response used by the MCP transport.
type MCPJSONRPCResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      any              `json:"id,omitempty"`
	Result  map[string]any   `json:"result,omitempty"`
	Error   *MCPJSONRPCError `json:"error,omitempty"`
}

// MCPJSONRPCError is a JSON-RPC 2.0 error object used by the MCP transport.
type MCPJSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// MCPClientInfo identifies the MCP client in an initialize request.
type MCPClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// MCPServerInfo identifies the MCP server in an initialize result.
type MCPServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// MCPCapabilities carries protocol capability flags.
type MCPCapabilities struct {
	Tools map[string]any `json:"tools,omitempty"`
}

// MCPInitializeRequest is the params for the initialize method.
type MCPInitializeRequest struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    MCPCapabilities `json:"capabilities"`
	ClientInfo      MCPClientInfo   `json:"clientInfo"`
}

// MCPInitializeResult is the result for the initialize method.
type MCPInitializeResult struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    MCPCapabilities `json:"capabilities"`
	ServerInfo      MCPServerInfo   `json:"serverInfo"`
}

// MCPToolInputSchema is a JSON Schema object describing a tool's arguments.
type MCPToolInputSchema struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Required   []string       `json:"required,omitempty"`
}

// MCPTool describes a single tool exposed by the MCP server.
type MCPTool struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	InputSchema MCPToolInputSchema `json:"inputSchema"`
}

// MCPToolsListResult is the result for tools/list.
type MCPToolsListResult struct {
	Tools []MCPTool `json:"tools"`
}

// MCPToolCallRequest is the params for tools/call.
type MCPToolCallRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// MCPContentType identifies the kind of content in a tool result.
type MCPContentType string

// MCP content types.
const (
	MCPContentTypeText MCPContentType = "text"
)

// MCPContent is a single content item in a tool result.
type MCPContent struct {
	Type MCPContentType `json:"type"`
	Text string         `json:"text"`
}

// MCPToolCallResult is the result for tools/call.
type MCPToolCallResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// RallyCreateToolArgs is the arguments map for the rally_create tool.
type RallyCreateToolArgs struct {
	Participants []string `json:"participants"`
	Repo         string   `json:"repo,omitempty"`
	Task         string   `json:"task,omitempty"`
	FirstHolder  string   `json:"first_holder,omitempty"`
}

// RallyJoinToolArgs is the arguments map for the rally_join tool.
type RallyJoinToolArgs struct {
	SessionID string `json:"session_id"`
	As        string `json:"as"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

// RallyDoneToolArgs is the arguments map for the rally_done tool.
type RallyDoneToolArgs struct {
	SessionID string `json:"session_id"`
	As        string `json:"as"`
	Note      string `json:"note,omitempty"`
	HandoffTo string `json:"handoff_to,omitempty"`
}

// RallyStatusToolArgs is the arguments map for the rally_status tool.
type RallyStatusToolArgs struct {
	SessionID string `json:"session_id"`
}

// RallyInterruptToolArgs is the arguments map for the rally_interrupt tool.
type RallyInterruptToolArgs struct {
	SessionID string `json:"session_id"`
}

// MCPTimeoutResult is returned by rally_join when the timeout fires first.
type MCPTimeoutResult struct {
	Timeout bool `json:"timeout"`
}
