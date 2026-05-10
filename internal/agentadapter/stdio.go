package agentadapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	maxStdioMessageBytes = 16 << 20
	maxHTTPResponseBytes = 32 << 20
)

type StdioOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
}

type stdioShim struct {
	cfg             Config
	client          *http.Client
	sessionID       string
	protocolVersion string
}

type rpcRequestEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

type rpcErrorResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   rpcError        `json:"error"`
}

type rpcError struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

// RunStdioShim reads newline-delimited stdio MCP JSON-RPC messages, forwards
// them to the configured Streamable HTTP route, and writes JSON-RPC responses
// back to stdout.
func RunStdioShim(ctx context.Context, cfg Config, opts StdioOptions) error {
	if err := ValidateConfig(cfg); err != nil {
		return err
	}
	if opts.Stdin == nil {
		return fmt.Errorf("stdin is required")
	}
	if opts.Stdout == nil {
		return fmt.Errorf("stdout is required")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: defaultHTTPClientLimit}
	}
	if strings.TrimSpace(cfg.ProtocolVersion) == "" {
		cfg.ProtocolVersion = DefaultProtocolVersion
	}

	shim := &stdioShim{
		cfg:             cfg,
		client:          cfg.HTTPClient,
		protocolVersion: cfg.ProtocolVersion,
	}

	scanner := bufio.NewScanner(opts.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), maxStdioMessageBytes)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		responses, err := shim.forward(ctx, append([]byte(nil), line...))
		if err != nil {
			return err
		}
		for _, response := range responses {
			if _, err := opts.Stdout.Write(response); err != nil {
				return err
			}
			if _, err := opts.Stdout.Write([]byte("\n")); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func (s *stdioShim) forward(ctx context.Context, payload []byte) ([][]byte, error) {
	envelope, hasResponseID, _ := parseRPCEnvelope(payload)
	if envelope.Method == "initialize" {
		if protocolVersion := protocolVersionFromInitialize(envelope.Params); protocolVersion != "" {
			s.protocolVersion = protocolVersion
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.RuntimeURL.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json, text/event-stream")
	req.Header.Set(MCPProtocolHeader, s.protocolVersion)
	if s.sessionID != "" {
		req.Header.Set(MCPSessionHeader, s.sessionID)
	}
	applyGovernanceHeaders(req.Header, s.cfg)
	if s.cfg.HostHeader != "" {
		req.Host = s.cfg.HostHeader
	}

	resp, err := s.client.Do(req)
	if err != nil {
		if hasResponseID {
			return [][]byte{jsonRPCHTTPError(envelope.ID, http.StatusBadGateway, err.Error(), nil)}, nil
		}
		return nil, nil
	}
	defer resp.Body.Close()

	if runtimeSessionID := resp.Header.Get(MCPSessionHeader); runtimeSessionID != "" {
		s.sessionID = runtimeSessionID
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxHTTPResponseBytes {
		if hasResponseID {
			return [][]byte{jsonRPCHTTPError(envelope.ID, http.StatusBadGateway, "upstream response too large", nil)}, nil
		}
		return nil, nil
	}
	body = bytes.TrimSpace(body)

	if resp.StatusCode >= http.StatusBadRequest {
		if len(body) > 0 && looksLikeJSONRPC(body) {
			return [][]byte{body}, nil
		}
		if hasResponseID {
			return [][]byte{jsonRPCHTTPError(envelope.ID, resp.StatusCode, extractHTTPErrorMessage(resp.StatusCode, body), body)}, nil
		}
		return nil, nil
	}
	if !hasResponseID {
		return nil, nil
	}
	if len(body) == 0 {
		return nil, nil
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("content-type")), "text/event-stream") {
		return decodeSSEDataMessages(body), nil
	}
	return [][]byte{body}, nil
}

func parseRPCEnvelope(payload []byte) (rpcRequestEnvelope, bool, error) {
	var envelope rpcRequestEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return envelope, false, err
	}
	return envelope, len(envelope.ID) > 0, nil
}

func protocolVersionFromInitialize(params json.RawMessage) string {
	if len(params) == 0 {
		return ""
	}
	var initParams initializeParams
	if err := json.Unmarshal(params, &initParams); err != nil {
		return ""
	}
	return strings.TrimSpace(initParams.ProtocolVersion)
}

func looksLikeJSONRPC(payload []byte) bool {
	var response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return false
	}
	return response.JSONRPC == "2.0" && (len(response.ID) > 0 || len(response.Result) > 0 || len(response.Error) > 0)
}

func extractHTTPErrorMessage(status int, payload []byte) string {
	if len(payload) > 0 {
		var object struct {
			Error any `json:"error"`
		}
		if err := json.Unmarshal(payload, &object); err == nil {
			switch value := object.Error.(type) {
			case string:
				if strings.TrimSpace(value) != "" {
					return value
				}
			case map[string]any:
				if message, ok := value["message"].(string); ok && strings.TrimSpace(message) != "" {
					return message
				}
			}
		}
		if text := strings.TrimSpace(string(payload)); text != "" {
			if len(text) > 240 {
				return text[:240]
			}
			return text
		}
	}
	if text := http.StatusText(status); text != "" {
		return text
	}
	return fmt.Sprintf("upstream HTTP %d", status)
}

func jsonRPCHTTPError(id json.RawMessage, status int, message string, payload []byte) []byte {
	response := rpcErrorResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: rpcError{
			Code:    -32000,
			Message: message,
			Data: map[string]any{
				"http_status": status,
			},
		},
	}
	if len(payload) > 0 && len(payload) <= 4096 {
		response.Error.Data["upstream_body"] = string(payload)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32000,"message":"upstream error"}}`)
	}
	return encoded
}

func decodeSSEDataMessages(payload []byte) [][]byte {
	var responses [][]byte
	var dataLines []string
	flush := func() {
		if len(dataLines) == 0 {
			return
		}
		data := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = nil
		if data == "" || data == "[DONE]" {
			return
		}
		if json.Valid([]byte(data)) {
			responses = append(responses, []byte(data))
		}
	}

	scanner := bufio.NewScanner(bytes.NewReader(payload))
	scanner.Buffer(make([]byte, 0, 64*1024), maxHTTPResponseBytes)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flush()
	return responses
}
