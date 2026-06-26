package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

const (
	ProtocolVersion = "2024-11-05"
	ServerName      = "correctover-mcp-server"
	ServerVersion   = "1.0.0"
)

type ToolHandler func(args map[string]any) (*ToolCallResult, error)

type Server struct {
	tools        map[string]Tool
	handlers     map[string]ToolHandler
	mu           sync.RWMutex
	logWriter    io.Writer
}

func NewServer() *Server {
	return &Server{
		tools:    make(map[string]Tool),
		handlers: make(map[string]ToolHandler),
	}
}

func (s *Server) SetLogWriter(w io.Writer) {
	s.logWriter = w
}

func (s *Server) log(msg string) {
	if s.logWriter != nil {
		fmt.Fprintf(s.logWriter, "[correctover] %s\n", msg)
	}
}

func (s *Server) RegisterTool(tool Tool, handler ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[tool.Name] = tool
	s.handlers[tool.Name] = handler
	s.log(fmt.Sprintf("registered tool: %s", tool.Name))
}

func (s *Server) Run() error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024) // 10MB max

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			s.log(fmt.Sprintf("parse error: %v", err))
			continue
		}

		resp := s.handleRequest(&req)
		if resp != nil {
			data, err := json.Marshal(resp)
			if err != nil {
				s.log(fmt.Sprintf("marshal error: %v", err))
				continue
			}
			fmt.Println(string(data))
		}
	}

	return scanner.Err()
}

func (s *Server) handleRequest(req *Request) *Response {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "initialized":
		return nil // notification, no response
	case "ping":
		return &Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	default:
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
		}
	}
}

func (s *Server) handleInitialize(req *Request) *Response {
	s.log("client initialized")
	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: InitializeResult{
			ProtocolVersion: ProtocolVersion,
			Capabilities: ServerCaps{
				Tools: &ToolsCap{ListChanged: false},
			},
			ServerInfo: Info{
				Name:    ServerName,
				Version: ServerVersion,
			},
			Instructions: "Correctover MCP Server — Real-time AI output verification and self-healing. " +
				"Routes LLM calls through multiple providers with 6-dimension output validation. " +
				"Configure your API keys via environment variables (OPENAI_API_KEY, ANTHROPIC_API_KEY, DEEPSEEK_API_KEY, etc.).",
		},
	}
}

func (s *Server) handleToolsList(req *Request) *Response {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]Tool, 0, len(s.tools))
	for _, t := range s.tools {
		tools = append(tools, t)
	}

	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  ToolsListResult{Tools: tools},
	}
}

func (s *Server) handleToolsCall(req *Request) *Response {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: -32602, Message: fmt.Sprintf("invalid params: %v", err)},
		}
	}

	s.mu.RLock()
	handler, ok := s.handlers[params.Name]
	s.mu.RUnlock()

	if !ok {
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: -32602, Message: fmt.Sprintf("unknown tool: %s", params.Name)},
		}
	}

	result, err := handler(params.Arguments)
	if err != nil {
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: &ToolCallResult{
				Content: []Content{TextContent(fmt.Sprintf("Error: %v", err))},
				IsError: true,
			},
		}
	}

	return &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}
