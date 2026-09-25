package agentadapter

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestRequestProtocolVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params string
		want   string
	}{
		{name: "modern", params: `{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}`, want: "2026-07-28"},
		{name: "legacy has no meta", params: `{"name":"echo"}`, want: ""},
		{name: "other meta keys only", params: `{"_meta":{"progressToken":1}}`, want: ""},
		{name: "non-string version", params: `{"_meta":{"io.modelcontextprotocol/protocolVersion":5}}`, want: ""},
		{name: "empty params", params: ``, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := requestProtocolVersion(json.RawMessage(tt.params)); got != tt.want {
				t.Fatalf("requestProtocolVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsModernProtocolVersion(t *testing.T) {
	t.Parallel()

	for version, want := range map[string]bool{
		"2026-07-28": true,
		"2027-01-01": true,
		"2025-11-25": false,
		"2025-06-18": false,
		"":           false,
		"latest":     false,
	} {
		if got := isModernProtocolVersion(version); got != want {
			t.Errorf("isModernProtocolVersion(%q) = %v, want %v", version, got, want)
		}
	}
}

func TestEncodeMCPHeaderValue(t *testing.T) {
	t.Parallel()

	// Cases from the 2026-07-28 Streamable HTTP "Value Encoding" table.
	tests := map[string]string{
		"us-west1":           "us-west1",
		"Hello, 世界":          "=?base64?SGVsbG8sIOS4lueVjA==?=",
		" padded ":           "=?base64?IHBhZGRlZCA=?=",
		"line1\nline2":       "=?base64?bGluZTEKbGluZTI=?=",
		"=?base64?literal?=": "=?base64?PT9iYXNlNjQ/bGl0ZXJhbD89?=",
		"file:///a/b c.json": "file:///a/b c.json",
	}
	for in, want := range tests {
		if got := encodeMCPHeaderValue(in); got != want {
			t.Errorf("encodeMCPHeaderValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestApplyModernRequestHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		method   string
		params   string
		wantName string
	}{
		{name: "tools/call", method: "tools/call", params: `{"name":"get_weather"}`, wantName: "get_weather"},
		{name: "prompts/get", method: "prompts/get", params: `{"name":"summarize"}`, wantName: "summarize"},
		{name: "resources/read", method: "resources/read", params: `{"uri":"file:///projects/app/config.json"}`, wantName: "file:///projects/app/config.json"},
		{name: "tools/list has no name", method: "tools/list", params: `{}`, wantName: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := http.Header{}
			applyModernRequestHeaders(header, tt.method, json.RawMessage(tt.params), nil)
			if got := header.Get(MCPMethodHeader); got != tt.method {
				t.Fatalf("Mcp-Method = %q, want %q", got, tt.method)
			}
			if got := header.Get(MCPNameHeader); got != tt.wantName {
				t.Fatalf("Mcp-Name = %q, want %q", got, tt.wantName)
			}
		})
	}
}

func TestHeaderBindingsFromSchema(t *testing.T) {
	t.Parallel()

	valid := `{"type":"object","properties":{
		"region":{"type":"string","x-mcp-header":"Region"},
		"opts":{"type":"object","properties":{"dry":{"type":"boolean","x-mcp-header":"Dry-Run"}}},
		"query":{"type":"string"}}}`
	got, err := headerBindingsFromSchema(json.RawMessage(valid))
	if err != nil {
		t.Fatalf("headerBindingsFromSchema(valid) error = %v", err)
	}
	want := []toolHeaderBinding{
		{header: "Dry-Run", path: []string{"opts", "dry"}},
		{header: "Region", path: []string{"region"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bindings = %#v, want %#v", got, want)
	}

	invalid := map[string]string{
		"under items":          `{"type":"object","properties":{"list":{"type":"array","items":{"type":"string","x-mcp-header":"Item"}}}}`,
		"under oneOf":          `{"type":"object","properties":{"v":{"oneOf":[{"type":"string","x-mcp-header":"V"}]}}}`,
		"number type":          `{"type":"object","properties":{"n":{"type":"number","x-mcp-header":"N"}}}`,
		"empty name":           `{"type":"object","properties":{"n":{"type":"string","x-mcp-header":""}}}`,
		"not a token":          `{"type":"object","properties":{"n":{"type":"string","x-mcp-header":"Bad Name"}}}`,
		"duplicate ignorecase": `{"type":"object","properties":{"a":{"type":"string","x-mcp-header":"Region"},"b":{"type":"string","x-mcp-header":"region"}}}`,
		"on root":              `{"type":"object","x-mcp-header":"Root"}`,
	}
	for name, schema := range invalid {
		if _, err := headerBindingsFromSchema(json.RawMessage(schema)); err == nil {
			t.Errorf("%s: headerBindingsFromSchema() error = nil, want error", name)
		}
	}
}

func TestFilterToolsListResultDropsInvalidTools(t *testing.T) {
	t.Parallel()

	body := []byte(`{"jsonrpc":"2.0","id":3,"result":{"resultType":"complete","tools":[
		{"name":"execute_sql","inputSchema":{"type":"object","properties":{"region":{"type":"string","x-mcp-header":"Region"}}}},
		{"name":"broken","inputSchema":{"type":"object","properties":{"n":{"type":"number","x-mcp-header":"N"}}}},
		{"name":"plain","inputSchema":{"type":"object"}}]}}`)

	filtered, bindings, rejected := filterToolsListResult(body)
	if len(rejected) != 1 || rejected[0].name != "broken" {
		t.Fatalf("rejected = %#v, want only broken", rejected)
	}
	if b := bindings["execute_sql"]; len(b) != 1 || b[0].header != "Region" {
		t.Fatalf("bindings[execute_sql] = %#v", b)
	}
	var decoded struct {
		ID     int `json:"id"`
		Result struct {
			ResultType string `json:"resultType"`
			Tools      []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(filtered, &decoded); err != nil {
		t.Fatalf("Unmarshal(filtered) error = %v", err)
	}
	var names []string
	for _, tool := range decoded.Result.Tools {
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "execute_sql,plain" || decoded.ID != 3 || decoded.Result.ResultType != "complete" {
		t.Fatalf("filtered = %s", filtered)
	}

	clean := []byte(`{"jsonrpc":"2.0","id":4,"result":{"tools":[{"name":"plain"}]}}`)
	if out, _, rejected := filterToolsListResult(clean); string(out) != string(clean) || len(rejected) != 0 {
		t.Fatalf("clean body changed: %s", out)
	}
}

func TestParamHeaderValues(t *testing.T) {
	t.Parallel()

	bindings := []toolHeaderBinding{
		{header: "Region", path: []string{"region"}},
		{header: "Limit", path: []string{"limit"}},
		{header: "Dry", path: []string{"opts", "dry"}},
		{header: "Missing", path: []string{"missing"}},
		{header: "Null", path: []string{"nothing"}},
	}
	args := json.RawMessage(`{"region":"us-west1","limit":42,"opts":{"dry":false},"nothing":null}`)
	got := paramHeaderValues(args, bindings)
	want := [][2]string{{"Region", "us-west1"}, {"Limit", "42"}, {"Dry", "false"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paramHeaderValues() = %v, want %v", got, want)
	}
}

// Value keywords hold instance data, not schemas: an "x-mcp-header" key
// inside them must not be treated as an (unreachable) annotation.
func TestHeaderBindingsFromSchemaIgnoresValueKeywords(t *testing.T) {
	t.Parallel()

	schema := `{"type":"object",
		"default":{"x-mcp-header":"Root-Default"},
		"examples":[{"cfg":{"x-mcp-header":"Example"}}],
		"x-vendor":{"x-mcp-header":"Vendor"},
		"properties":{
			"region":{"type":"string","x-mcp-header":"Region"},
			"cfg":{"type":"object",
				"default":{"x-mcp-header":"Default"},
				"const":{"x-mcp-header":"Const"},
				"enum":[{"x-mcp-header":"Enum"}],
				"properties":{"mode":{"type":"string","default":"x","examples":[{"properties":{"a":{"x-mcp-header":"Nested"}}}]}}}}}`
	got, err := headerBindingsFromSchema(json.RawMessage(schema))
	if err != nil {
		t.Fatalf("headerBindingsFromSchema() error = %v, want nil", err)
	}
	want := []toolHeaderBinding{{header: "Region", path: []string{"region"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bindings = %#v, want %#v", got, want)
	}

	// Subschema keywords are still walked and rejected as unreachable.
	for name, schema := range map[string]string{
		"under $defs":                `{"type":"object","$defs":{"d":{"type":"string","x-mcp-header":"D"}}}`,
		"under additionalProperties": `{"type":"object","additionalProperties":{"type":"string","x-mcp-header":"A"}}`,
		"under not":                  `{"type":"object","properties":{"v":{"not":{"type":"string","x-mcp-header":"V"}}}}`,
	} {
		if _, err := headerBindingsFromSchema(json.RawMessage(schema)); err == nil {
			t.Errorf("%s: headerBindingsFromSchema() error = nil, want error", name)
		}
	}
}

func TestFilterToolsListResultKeepsToolWithHeaderKeyInDefault(t *testing.T) {
	t.Parallel()

	body := []byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[
		{"name":"configure","inputSchema":{"type":"object","properties":{"headers":{"type":"object","default":{"x-mcp-header":"literal"}}}}}]}}`)
	out, _, rejected := filterToolsListResult(body)
	if len(rejected) != 0 || string(out) != string(body) {
		t.Fatalf("rejected = %#v, out = %s; want tool kept", rejected, out)
	}
}
