package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/version"
)

// Server runs an MCP stdio JSON-RPC server for keycli.
type Server struct {
	app *app.App
	in  io.Reader
	out io.Writer
	mu  sync.Mutex
}

// NewServer creates a new MCP server.
func NewServer(a *app.App, in io.Reader, out io.Writer) *Server {
	return &Server{
		app: a,
		in:  in,
		out: out,
	}
}

// Run starts the JSON-RPC event loop until EOF or context cancellation.
func (s *Server) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(s.in)
	// Allow large requests if necessary
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			s.sendError(nil, ErrCodeParseError, "Parse error: invalid JSON", err.Error())
			continue
		}

		if req.JSONRPC != JSONRPCVersion && req.JSONRPC != "" {
			s.sendError(req.ID, ErrCodeInvalidRequest, "Invalid JSON-RPC version", nil)
			continue
		}

		s.handleRequest(ctx, req)
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		return err
	}
	return nil
}

func (s *Server) handleRequest(ctx context.Context, req Request) {
	switch req.Method {
	case "initialize":
		res := InitializeResult{
			ProtocolVersion: ProtocolVersion,
			Capabilities: ServerCapabilities{
				Tools: map[string]any{},
			},
			ServerInfo: ServerInfo{
				Name:    "keycli",
				Version: version.Version,
			},
		}
		s.sendResult(req.ID, res)

	case "notifications/initialized":
		// Notification from client; no response needed.

	case "ping":
		s.sendResult(req.ID, map[string]any{})

	case "tools/list":
		tools := GetToolDefinitions()
		s.sendResult(req.ID, ListToolsResult{Tools: tools})

	case "tools/call":
		var params CallToolParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.sendError(req.ID, ErrCodeInvalidParams, "Invalid tool call parameters", err.Error())
			return
		}

		result, err := ExecuteTool(ctx, s.app, params.Name, params.Arguments)
		if err != nil {
			s.sendError(req.ID, ErrCodeInternalError, fmt.Sprintf("Tool execution error: %v", err), nil)
			return
		}
		s.sendResult(req.ID, result)

	default:
		if req.ID != nil {
			s.sendError(req.ID, ErrCodeMethodNotFound, fmt.Sprintf("Method not found: %s", req.Method), nil)
		}
	}
}

func (s *Server) sendResult(id any, result any) {
	resp := Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result:  result,
	}
	s.writeResponse(resp)
}

func (s *Server) sendError(id any, code int, msg string, data any) {
	resp := Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: msg,
			Data:    data,
		},
	}
	s.writeResponse(resp)
}

func (s *Server) writeResponse(resp Response) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_, _ = s.out.Write(b)
	_, _ = s.out.Write([]byte("\n"))
}
