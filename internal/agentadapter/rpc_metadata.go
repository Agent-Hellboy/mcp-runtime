package agentadapter

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const maxLogFieldBytes = 120

type rpcRequestMetadata struct {
	ID       json.RawMessage
	HasID    bool
	Method   string
	ToolName string
}

func parseRPCRequestMetadata(payload []byte) rpcRequestMetadata {
	envelope, hasID, err := parseRPCEnvelope(payload)
	if err != nil {
		return rpcRequestMetadata{}
	}
	meta := rpcRequestMetadata{
		HasID:    hasID,
		Method:   envelope.Method,
		ToolName: toolNameFromRPCParams(envelope.Method, envelope.Params),
	}
	if len(envelope.ID) > 0 {
		meta.ID = append(json.RawMessage(nil), envelope.ID...)
	}
	return meta
}

func rpcIDOrNull(meta rpcRequestMetadata) json.RawMessage {
	if meta.HasID && len(meta.ID) > 0 {
		return meta.ID
	}
	return json.RawMessage("null")
}

func toolNameFromRPCParams(method string, params json.RawMessage) string {
	if method != "tools/call" || len(params) == 0 {
		return ""
	}
	var toolCall struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(params, &toolCall); err != nil {
		return ""
	}
	return strings.TrimSpace(toolCall.Name)
}

func logRuntimeDenial(logLevel string, logWriter io.Writer, component string, status int, message string, meta rpcRequestMetadata) {
	if status < http.StatusBadRequest || status >= http.StatusInternalServerError {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(logLevel), "info") {
		return
	}
	writer := logWriter
	if writer == nil {
		writer = os.Stderr
	}

	fmt.Fprintf(writer, "%s: %d %s", component, status, sanitizeLogField(message))
	if meta.Method != "" {
		fmt.Fprintf(writer, " method=%s", sanitizeLogField(meta.Method))
	}
	if meta.ToolName != "" {
		fmt.Fprintf(writer, " tool=%s", sanitizeLogField(meta.ToolName))
	}
	fmt.Fprintln(writer)
}

func sanitizeLogField(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), "_")
	if value == "" {
		return "unknown"
	}
	if len(value) > maxLogFieldBytes {
		return value[:maxLogFieldBytes]
	}
	return value
}

func closeIfPossible(reader io.Reader) {
	if closer, ok := reader.(io.Closer); ok {
		_ = closer.Close()
	}
}
