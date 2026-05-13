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
	"sync"
)

const (
	maxStdioMessageBytes = 16 << 20
	maxHTTPResponseBytes = 32 << 20
)

var eventStreamDataPrefix = []byte("data:")

type StdioOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
}

// sessionState tracks the lifecycle of the MCP session within the stdio shim.
type sessionState uint8

const (
	// sessionStateRequired is the default: a governed identity and a successful
	// initialize are required before non-handshake requests are forwarded.
	sessionStateRequired sessionState = iota
	// sessionStateOptional is set when Anonymous is true: the shim forwards
	// requests without an issued identity or session ID.
	sessionStateOptional
	// sessionStateReady means initialize succeeded and (if the runtime returned
	// one) the Mcp-Session-Id header has been captured.
	sessionStateReady
	// sessionStateFailed means initialize returned an HTTP error or a transport
	// error. Subsequent non-initialize requests are rejected with a JSON-RPC
	// error rather than forwarded.
	sessionStateFailed
)

type stdioShim struct {
	cfg             ShimConfig
	client          *http.Client
	mu              sync.Mutex
	sessionSt       sessionState
	sessionID       string
	protocolVersion string
}

type stdioScanResult struct {
	line []byte
	err  error
	done bool
}

type stdioResponseEmitter func([]byte) error

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
func RunStdioShim(ctx context.Context, cfg ShimConfig, opts StdioOptions) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if opts.Stdin == nil {
		return fmt.Errorf("stdin is required")
	}
	if opts.Stdout == nil {
		return fmt.Errorf("stdout is required")
	}
	if strings.TrimSpace(cfg.ProtocolVersion) == "" {
		cfg.ProtocolVersion = DefaultProtocolVersion
	}
	if cfg.Transport == nil {
		cfg.Transport = &RuntimeTransport{}
	}

	initState := sessionStateRequired
	if cfg.Anonymous {
		initState = sessionStateOptional
	}
	shim := &stdioShim{
		cfg:             cfg,
		client:          cfg.Transport.Client(),
		sessionSt:       initState,
		protocolVersion: cfg.ProtocolVersion,
	}

	scanResults := scanStdioLines(ctx, opts.Stdin)
	var stdoutMu sync.Mutex
	emit := func(response []byte) error {
		stdoutMu.Lock()
		defer stdoutMu.Unlock()
		if _, err := opts.Stdout.Write(response); err != nil {
			return err
		}
		if _, err := opts.Stdout.Write([]byte("\n")); err != nil {
			return err
		}
		return nil
	}
	// tracker lets shutdown cancel every in-flight forward goroutine before
	// the WaitGroup resolves, so client.Do calls unblock quickly.
	tracker := newRequestTracker()
	var forwards sync.WaitGroup
	errCh := make(chan error, 1)
	sendErr := func(err error) {
		if err == nil {
			return
		}
		select {
		case errCh <- err:
		default:
		}
	}
	for {
		select {
		case <-ctx.Done():
			closeIfPossible(opts.Stdin)
			tracker.cancelAll(context.Cause(ctx))
			forwards.Wait()
			return nil
		case err := <-errCh:
			closeIfPossible(opts.Stdin)
			tracker.cancelAll(err)
			forwards.Wait()
			return err
		case result := <-scanResults:
			if result.done {
				if result.err != nil {
					if ctx.Err() != nil {
						return nil
					}
					return result.err
				}
				forwards.Wait()
				select {
				case err := <-errCh:
					return err
				default:
				}
				return nil
			}
			line := bytes.TrimSpace(result.line)
			if len(line) == 0 {
				continue
			}
			payload := append([]byte(nil), line...)
			if parseRPCRequestMetadata(payload).Method == "initialize" {
				if err := shim.forward(ctx, payload, emit); err != nil {
					if ctx.Err() != nil {
						return nil
					}
					return err
				}
				continue
			}
			forwards.Add(1)
			go func() {
				defer forwards.Done()
				fwdCtx, id := tracker.track(ctx)
				defer tracker.done(id)
				if err := shim.forward(fwdCtx, payload, emit); err != nil && ctx.Err() == nil {
					sendErr(err)
				}
			}()
		}
	}
}

func scanStdioLines(ctx context.Context, stdin io.Reader) <-chan stdioScanResult {
	results := make(chan stdioScanResult, 1)
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), maxStdioMessageBytes)
	go func() {
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			select {
			case results <- stdioScanResult{line: line}:
			case <-ctx.Done():
				return
			}
		}
		select {
		case results <- stdioScanResult{err: scanner.Err(), done: true}:
		case <-ctx.Done():
		}
	}()
	return results
}

func (s *stdioShim) forward(ctx context.Context, payload []byte, emit stdioResponseEmitter) error {
	meta := parseRPCRequestMetadata(payload)
	envelope, hasResponseID, parseErr := parseRPCEnvelope(payload)
	if parseErr != nil {
		return emit(jsonRPCParseError(parseErr.Error()))
	}

	// Session state and allowlist checks — exempt protocol handshake messages.
	if meta.Method != "initialize" && meta.Method != "notifications/initialized" {
		if s.getSessionState() == sessionStateFailed {
			if hasResponseID {
				return emit(jsonRPCSessionFailedError(envelope.ID))
			}
			return nil
		}
		if !s.isMethodAllowed(meta.Method) {
			if hasResponseID {
				return emit(jsonRPCMethodNotAllowedError(envelope.ID, meta.Method))
			}
			return nil
		}
	}

	protocolVersion, sessionID := s.prepareRequestState(envelope)

	// Tag context with method so RuntimeTransport can key retry and OTel on it.
	ctx = withRPCMethod(ctx, meta.Method)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.RuntimeURL.String(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json, text/event-stream")
	req.Header.Set(MCPProtocolHeader, protocolVersion)
	if sessionID != "" {
		req.Header.Set(MCPSessionHeader, sessionID)
	}
	s.cfg.Identity.Apply(req.Header)
	if s.cfg.HostHeader != "" {
		req.Host = s.cfg.HostHeader
	}

	resp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		if meta.Method == "initialize" {
			s.setSessionState(sessionStateFailed)
		}
		if hasResponseID {
			return emit(jsonRPCHTTPError(envelope.ID, http.StatusBadGateway, err.Error(), nil))
		}
		return nil
	}
	defer resp.Body.Close()

	if runtimeSessionID := resp.Header.Get(MCPSessionHeader); runtimeSessionID != "" {
		s.setRuntimeSessionID(runtimeSessionID)
	}

	if resp.StatusCode < http.StatusBadRequest && strings.Contains(strings.ToLower(resp.Header.Get("content-type")), "text/event-stream") {
		return streamStreamableHTTPEventMessages(resp.Body, emit)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPResponseBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxHTTPResponseBytes {
		if hasResponseID {
			return emit(jsonRPCHTTPError(envelope.ID, http.StatusBadGateway, "upstream response too large", nil))
		}
		return nil
	}
	body = bytes.TrimSpace(body)

	if resp.StatusCode >= http.StatusBadRequest {
		if meta.Method == "initialize" {
			s.setSessionState(sessionStateFailed)
		}
		logRuntimeDenial(s.cfg.LogLevel, s.cfg.LogWriter, "adapter/stdio", resp.StatusCode, extractHTTPErrorMessage(resp.StatusCode, body), meta)
		if isSessionExpiredBody(body) {
			if hasResponseID {
				return emit(jsonRPCSessionExpiredError(envelope.ID, extractHTTPErrorMessage(resp.StatusCode, body)))
			}
			return nil
		}
		if len(body) > 0 && looksLikeJSONRPC(body) {
			return emit(body)
		}
		if hasResponseID {
			return emit(jsonRPCHTTPError(envelope.ID, resp.StatusCode, extractHTTPErrorMessage(resp.StatusCode, body), body))
		}
		return nil
	}
	if meta.Method == "initialize" {
		// A 2xx response with a JSON-RPC error body (e.g. protocol mismatch
		// returned in-band) counts as a failed session so subsequent calls are
		// not forwarded without a working session.
		if looksLikeJSONRPCError(body) {
			s.setSessionState(sessionStateFailed)
		} else {
			s.setSessionState(sessionStateReady)
		}
	}
	if !hasResponseID {
		return nil
	}
	if len(body) == 0 {
		return nil
	}
	return emit(body)
}

func (s *stdioShim) prepareRequestState(envelope rpcRequestEnvelope) (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if envelope.Method == "initialize" {
		if protocolVersion := protocolVersionFromInitialize(envelope.Params); protocolVersion != "" {
			s.protocolVersion = protocolVersion
		}
	}
	return s.protocolVersion, s.sessionID
}

func (s *stdioShim) setRuntimeSessionID(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionID = sessionID
}

func (s *stdioShim) getSessionState() sessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionSt
}

func (s *stdioShim) setSessionState(st sessionState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionSt = st
}

// isMethodAllowed returns true when method is in the configured (or default)
// anonymous method allowlist. Always returns true when Anonymous is false.
func (s *stdioShim) isMethodAllowed(method string) bool {
	if !s.cfg.Anonymous {
		return true
	}
	list := s.cfg.AnonymousMethods
	if len(list) == 0 {
		list = DefaultAnonymousMethods
	}
	for _, m := range list {
		if m == method {
			return true
		}
	}
	return false
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

func looksLikeJSONRPCError(payload []byte) bool {
	var response struct {
		JSONRPC string          `json:"jsonrpc"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return false
	}
	return response.JSONRPC == "2.0" && len(response.Error) > 0
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

func jsonRPCParseError(detail string) []byte {
	response := rpcErrorResponse{
		JSONRPC: "2.0",
		ID:      json.RawMessage("null"),
		Error: rpcError{
			Code:    -32700,
			Message: "parse error",
			Data: map[string]any{
				"detail": detail,
			},
		},
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"parse error"}}`)
	}
	return encoded
}

func jsonRPCSessionFailedError(id json.RawMessage) []byte {
	response := rpcErrorResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: rpcError{
			Code:    -32000,
			Message: "session not established: initialize failed or was not attempted",
		},
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"session not established"}}`, string(id)))
	}
	return encoded
}

func jsonRPCMethodNotAllowedError(id json.RawMessage, method string) []byte {
	response := rpcErrorResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: rpcError{
			Code:    -32601,
			Message: "method not allowed in anonymous mode: " + method,
		},
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not allowed"}}`, string(id)))
	}
	return encoded
}

func jsonRPCSessionExpiredError(id json.RawMessage, message string) []byte {
	response := rpcErrorResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: rpcError{
			Code:    -32000,
			Message: message,
			Data: map[string]any{
				"runtime_status": "session_expired",
			},
		},
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"session expired","data":{"runtime_status":"session_expired"}}}`, string(id)))
	}
	return encoded
}

func decodeStreamableHTTPEventMessages(payload []byte) [][]byte {
	var responses [][]byte
	_ = scanStreamableHTTPEventMessages(bytes.NewReader(payload), func(data []byte) error {
		responses = append(responses, append([]byte(nil), data...))
		return nil
	})
	return responses
}

func streamStreamableHTTPEventMessages(payload io.Reader, emit stdioResponseEmitter) error {
	return scanStreamableHTTPEventMessages(payload, emit)
}

func scanStreamableHTTPEventMessages(payload io.Reader, emit stdioResponseEmitter) error {
	var dataLines []string
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = nil
		if data == "" || data == "[DONE]" {
			return nil
		}
		if json.Valid([]byte(data)) {
			return emit([]byte(data))
		}
		return nil
	}

	scanner := bufio.NewScanner(payload)
	scanner.Buffer(make([]byte, 0, 64*1024), maxHTTPResponseBytes)
	for scanner.Scan() {
		line := bytes.TrimRight(scanner.Bytes(), "\r")
		if len(line) == 0 {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if bytes.HasPrefix(line, eventStreamDataPrefix) {
			dataLines = append(dataLines, string(bytes.TrimSpace(bytes.TrimPrefix(line, eventStreamDataPrefix))))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}
