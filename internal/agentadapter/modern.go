package agentadapter

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// MCP protocol revision 2026-07-28 removed the initialize handshake. Every
// request carries its protocol version in params._meta, and Streamable HTTP
// requests mirror selected body fields into headers that servers validate
// against the body. See
// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#request-metadata.

const (
	// ModernProtocolVersion is the first MCP revision that uses per-request
	// metadata instead of an initialize handshake.
	ModernProtocolVersion = "2026-07-28"

	// MetaProtocolVersionKey is the params._meta key carrying a request's
	// protocol version in modern revisions.
	MetaProtocolVersionKey = "io.modelcontextprotocol/protocolVersion"

	MCPMethodHeader      = "Mcp-Method"
	MCPNameHeader        = "Mcp-Name"
	MCPParamHeaderPrefix = "Mcp-Param-"

	headerBase64Prefix = "=?base64?"
	headerBase64Suffix = "?="
)

// requestProtocolVersion returns params._meta["io.modelcontextprotocol/protocolVersion"],
// or "" when the request does not declare one (legacy requests).
func requestProtocolVersion(params json.RawMessage) string {
	if len(params) == 0 {
		return ""
	}
	var p struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ""
	}
	raw, ok := p.Meta[MetaProtocolVersionKey]
	if !ok {
		return ""
	}
	var version string
	if err := json.Unmarshal(raw, &version); err != nil {
		return ""
	}
	return strings.TrimSpace(version)
}

// isModernProtocolVersion reports whether version is 2026-07-28 or later.
// Protocol versions are YYYY-MM-DD strings, so lexical order is date order.
func isModernProtocolVersion(version string) bool {
	return len(version) == len(ModernProtocolVersion) && version >= ModernProtocolVersion
}

// encodeMCPHeaderValue returns value unchanged when it is a safe plain-ASCII
// header value, and the =?base64?...?= sentinel form otherwise. Values that
// already look like the sentinel are always encoded to avoid ambiguity.
func encodeMCPHeaderValue(value string) string {
	if isPlainHeaderValue(value) && !(strings.HasPrefix(value, headerBase64Prefix) && strings.HasSuffix(value, headerBase64Suffix)) {
		return value
	}
	return headerBase64Prefix + base64.StdEncoding.EncodeToString([]byte(value)) + headerBase64Suffix
}

// isPlainHeaderValue reports whether value consists of visible ASCII, space,
// and tab, with no leading or trailing whitespace (RFC 9110 field values).
func isPlainHeaderValue(value string) bool {
	if value == "" {
		return true
	}
	if first, last := value[0], value[len(value)-1]; first == ' ' || first == '\t' || last == ' ' || last == '\t' {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c != '\t' && (c < 0x20 || c > 0x7e) {
			return false
		}
	}
	return true
}

// mcpNameSource returns the body value mirrored into Mcp-Name for methods that
// require it: params.name for tools/call and prompts/get, params.uri for
// resources/read.
func mcpNameSource(method string, params json.RawMessage) (string, bool) {
	var field string
	switch method {
	case "tools/call", "prompts/get":
		field = "name"
	case "resources/read":
		field = "uri"
	default:
		return "", false
	}
	var p map[string]json.RawMessage
	if err := json.Unmarshal(params, &p); err != nil {
		return "", false
	}
	raw, ok := p[field]
	if !ok {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

// applyModernRequestHeaders sets Mcp-Method, Mcp-Name, and Mcp-Param-* headers
// for a modern request. index may be nil.
func applyModernRequestHeaders(header http.Header, method string, params json.RawMessage, index *toolHeaderIndex) {
	if method != "" && isPlainHeaderValue(method) {
		header.Set(MCPMethodHeader, method)
	}
	name, ok := mcpNameSource(method, params)
	if !ok {
		return
	}
	header.Set(MCPNameHeader, encodeMCPHeaderValue(name))
	if method != "tools/call" || index == nil {
		return
	}
	bindings := index.bindings(name)
	if len(bindings) == 0 {
		return
	}
	var call struct {
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return
	}
	for _, kv := range paramHeaderValues(call.Arguments, bindings) {
		header.Set(MCPParamHeaderPrefix+kv[0], encodeMCPHeaderValue(kv[1]))
	}
}

// toolHeaderBinding maps one x-mcp-header annotation to the property path of
// the annotated argument.
type toolHeaderBinding struct {
	header string
	path   []string
}

// toolHeaderIndex remembers the x-mcp-header bindings of tools seen in
// modern tools/list results so tools/call requests can mirror them.
type toolHeaderIndex struct {
	mu    sync.RWMutex
	tools map[string][]toolHeaderBinding
}

func newToolHeaderIndex() *toolHeaderIndex {
	return &toolHeaderIndex{tools: make(map[string][]toolHeaderBinding)}
}

func (x *toolHeaderIndex) bindings(tool string) []toolHeaderBinding {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.tools[tool]
}

// update records bindings per tool. tools/list can be paginated, so entries
// from earlier pages are kept until invalidate.
func (x *toolHeaderIndex) update(tools map[string][]toolHeaderBinding) {
	x.mu.Lock()
	defer x.mu.Unlock()
	for name, b := range tools {
		x.tools[name] = b
	}
}

func (x *toolHeaderIndex) invalidate() {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.tools = make(map[string][]toolHeaderBinding)
}

// rejectedTool names a tool dropped from a tools/list result and why.
type rejectedTool struct {
	name   string
	reason string
}

// filterToolsListResult validates x-mcp-header annotations in a tools/list
// response. It returns the response with invalid tools removed (the input
// unchanged when nothing was removed or the body is not a tools/list result),
// the bindings of the valid tools, and the rejected tools.
func filterToolsListResult(body []byte) ([]byte, map[string][]toolHeaderBinding, []rejectedTool) {
	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil {
		return body, nil, nil
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(response["result"], &result); err != nil {
		return body, nil, nil
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(result["tools"], &tools); err != nil {
		return body, nil, nil
	}
	bindings := make(map[string][]toolHeaderBinding, len(tools))
	kept := make([]json.RawMessage, 0, len(tools))
	var rejected []rejectedTool
	for _, raw := range tools {
		var tool struct {
			Name        string          `json:"name"`
			InputSchema json.RawMessage `json:"inputSchema"`
		}
		if err := json.Unmarshal(raw, &tool); err != nil {
			kept = append(kept, raw)
			continue
		}
		b, err := headerBindingsFromSchema(tool.InputSchema)
		if err != nil {
			rejected = append(rejected, rejectedTool{name: tool.Name, reason: err.Error()})
			continue
		}
		bindings[tool.Name] = b
		kept = append(kept, raw)
	}
	if len(rejected) == 0 {
		return body, bindings, nil
	}
	encodedTools, err := json.Marshal(kept)
	if err != nil {
		return body, bindings, rejected
	}
	result["tools"] = encodedTools
	encodedResult, err := json.Marshal(result)
	if err != nil {
		return body, bindings, rejected
	}
	response["result"] = encodedResult
	out, err := json.Marshal(response)
	if err != nil {
		return body, bindings, rejected
	}
	return out, bindings, rejected
}

// headerBindingsFromSchema collects x-mcp-header annotations from a tool
// inputSchema and returns an error when any annotation violates the
// 2026-07-28 constraints: a non-empty token name, unique case-insensitively,
// on an integer, string, or boolean property reachable from the root through
// "properties" keys only.
func headerBindingsFromSchema(schema json.RawMessage) ([]toolHeaderBinding, error) {
	if len(bytes.TrimSpace(schema)) == 0 {
		return nil, nil
	}
	var root any
	if err := json.Unmarshal(schema, &root); err != nil {
		return nil, nil
	}
	var out []toolHeaderBinding
	seen := map[string]bool{}
	var walk func(node any, path []string, reachable bool) error
	walk = func(node any, path []string, reachable bool) error {
		switch n := node.(type) {
		case map[string]any:
			if raw, ok := n["x-mcp-header"]; ok {
				if !reachable || len(path) == 0 {
					return errors.New("x-mcp-header must annotate a property reachable through \"properties\" only")
				}
				name, _ := raw.(string)
				if name == "" || !isHeaderToken(name) {
					return fmt.Errorf("x-mcp-header %q is not a valid header name token", name)
				}
				switch typ, _ := n["type"].(string); typ {
				case "integer", "string", "boolean":
				default:
					return fmt.Errorf("x-mcp-header %q annotates a property of type %q; only integer, string, and boolean are allowed", name, typ)
				}
				lower := strings.ToLower(name)
				if seen[lower] {
					return fmt.Errorf("x-mcp-header %q is not unique", name)
				}
				seen[lower] = true
				out = append(out, toolHeaderBinding{header: name, path: append([]string(nil), path...)})
			}
			for key, child := range n {
				if key == "x-mcp-header" {
					continue
				}
				if props, ok := child.(map[string]any); ok && key == "properties" {
					for prop, sub := range props {
						if err := walk(sub, append(append([]string(nil), path...), prop), reachable); err != nil {
							return err
						}
					}
					continue
				}
				if err := walk(child, path, false); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range n {
				if err := walk(child, path, false); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root, nil, true); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].header < out[j].header })
	return out, nil
}

// isHeaderToken reports whether s matches the RFC 9110 token syntax (1*tchar).
func isHeaderToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// paramHeaderValues returns (header name suffix, value) pairs for the bound
// arguments that are present and non-null.
func paramHeaderValues(arguments json.RawMessage, bindings []toolHeaderBinding) [][2]string {
	if len(bindings) == 0 || len(arguments) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(arguments))
	decoder.UseNumber()
	var args any
	if err := decoder.Decode(&args); err != nil {
		return nil
	}
	var out [][2]string
	for _, b := range bindings {
		value, ok := lookupPath(args, b.path)
		if !ok {
			continue
		}
		if text, ok := headerTextForValue(value); ok {
			out = append(out, [2]string{b.header, text})
		}
	}
	return out
}

func lookupPath(node any, path []string) (any, bool) {
	for _, key := range path {
		object, ok := node.(map[string]any)
		if !ok {
			return nil, false
		}
		if node, ok = object[key]; !ok {
			return nil, false
		}
	}
	return node, node != nil
}

func headerTextForValue(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return strconv.FormatInt(n, 10), true
		}
		f, err := v.Float64()
		if err != nil || f != math.Trunc(f) || math.Abs(f) > 1<<53-1 {
			return "", false
		}
		return strconv.FormatInt(int64(f), 10), true
	default:
		return "", false
	}
}
