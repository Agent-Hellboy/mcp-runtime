package operator

import (
	"errors"
	"testing"

	"mcp-runtime/pkg/errx"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
)

func TestErrors(t *testing.T) {
	err := wrapOperatorError(ErrReconcileDeployment, "test", map[string]any{
		"mcpServer": "test",
		"namespace": "test",
		"resource":  "test",
		"operation": "test",
	})
	if errx.UserString(err) != "test" {
		t.Errorf("UserString(err) = %q, want %q", errx.UserString(err), "test")
	}
	if errx.DebugString(err) != "1: *errx.Error: test | code=73000 | description=\"Operator error\" | message=\"test\" | context={mcpServer=test, namespace=test, resource=test, operation=test}" {
		t.Errorf("DebugString(err) = %q, want %q", errx.DebugString(err), "1: *errx.Error: test | code=73000 | description=\"Operator error\" | message=\"test\" | context={mcpServer=test, namespace=test, resource=test, operation=test}")
	}
}

func TestErrors_NewOperatorError(t *testing.T) {
	err := newOperatorError("test", map[string]any{
		"mcpServer": "test",
		"namespace": "test",
		"resource":  "test",
		"operation": "test",
	})
	if errx.UserString(err) != "test" {
		t.Errorf("UserString(err) = %q, want %q", errx.UserString(err), "test")
	}
	if errx.DebugString(err) != "1: *errx.Error: test | code=73000 | description=\"Operator error\" | message=\"test\" | context={mcpServer=test, namespace=test, resource=test, operation=test}" {
		t.Errorf("DebugString(err) = %q, want %q", errx.DebugString(err), "1: *errx.Error: test | code=73000 | description=\"Operator error\" | message=\"test\" | context={mcpServer=test, namespace=test, resource=test, operation=test}")
	}
}

// testLogger is a simple logger that captures log calls for testing.
type testLogger struct {
	errorCalls []errorCall
}

type errorCall struct {
	err           error
	msg           string
	keysAndValues []interface{}
}

func (l *testLogger) Init(info logr.RuntimeInfo) {}

func (l *testLogger) Enabled(level int) bool {
	return true
}

func (l *testLogger) Info(level int, msg string, keysAndValues ...interface{}) {}

func (l *testLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	l.errorCalls = append(l.errorCalls, errorCall{
		err:           err,
		msg:           msg,
		keysAndValues: keysAndValues,
	})
}

func (l *testLogger) WithValues(keysAndValues ...interface{}) logr.LogSink {
	return l
}

func (l *testLogger) WithName(name string) logr.LogSink {
	return l
}

func TestErrors_LogOperatorError(t *testing.T) {
	t.Run("with errx.Error and context", func(t *testing.T) {
		testLog := &testLogger{}
		logger := logr.New(testLog)
		err := errx.Operator("Failed to reconcile Deployment").
			WithContextMap(map[string]any{
				"mcpServer": "my-server",
				"namespace": "mcp-servers",
				"resource":  "deployment",
			})

		logOperatorError(logger, err, "Failed to reconcile Deployment")

		assert.Len(t, testLog.errorCalls, 1, "should log exactly one error")
		call := testLog.errorCalls[0]
		assert.Equal(t, err, call.err, "logged error should match")
		assert.Equal(t, "Failed to reconcile Deployment", call.msg, "logged message should match")

		// Verify structured fields are present
		kv := call.keysAndValues
		assert.Contains(t, kv, "error.code", "should include error.code")
		assert.Contains(t, kv, "error.category", "should include error.category")
		assert.Contains(t, kv, "error.message", "should include error.message")
		assert.Contains(t, kv, "error.context.mcpServer", "should include context.mcpServer")
		assert.Contains(t, kv, "error.context.namespace", "should include context.namespace")
		assert.Contains(t, kv, "error.context.resource", "should include context.resource")

		// Verify values
		assert.Equal(t, errx.CodeOperator, getValue(kv, "error.code"), "error.code should match")
		assert.Equal(t, errx.DescOperator, getValue(kv, "error.category"), "error.category should match")
		assert.Equal(t, "Failed to reconcile Deployment", getValue(kv, "error.message"), "error.message should match")
		assert.Equal(t, "my-server", getValue(kv, "error.context.mcpServer"), "context.mcpServer should match")
		assert.Equal(t, "mcp-servers", getValue(kv, "error.context.namespace"), "context.namespace should match")
		assert.Equal(t, "deployment", getValue(kv, "error.context.resource"), "context.resource should match")
	})

	t.Run("with errx.Error and cause", func(t *testing.T) {
		testLog := &testLogger{}
		logger := logr.New(testLog)
		cause := errors.New("underlying error")
		err := errx.WrapOperator("Failed to reconcile", cause).
			WithContextMap(map[string]any{
				"mcpServer": "my-server",
			})

		logOperatorError(logger, err, "Failed to reconcile")

		assert.Len(t, testLog.errorCalls, 1, "should log exactly one error")
		call := testLog.errorCalls[0]
		kv := call.keysAndValues

		// Verify cause is logged
		assert.Contains(t, kv, "error.cause", "should include error.cause")
		assert.Equal(t, "underlying error", getValue(kv, "error.cause"), "error.cause should match")
	})

	t.Run("with non-errx error (fallback)", func(t *testing.T) {
		testLog := &testLogger{}
		logger := logr.New(testLog)
		err := errors.New("standard error")

		logOperatorError(logger, err, "Standard error occurred")

		assert.Len(t, testLog.errorCalls, 1, "should log exactly one error")
		call := testLog.errorCalls[0]
		assert.Equal(t, err, call.err, "logged error should match")
		assert.Equal(t, "Standard error occurred", call.msg, "logged message should match")
		assert.Empty(t, call.keysAndValues, "should not have structured fields for non-errx errors")
	})

	t.Run("with nil error (early return)", func(t *testing.T) {
		testLog := &testLogger{}
		logger := logr.New(testLog)

		logOperatorError(logger, nil, "Should not log")

		assert.Len(t, testLog.errorCalls, 0, "should not log when error is nil")
	})

	t.Run("with errx.Error without context", func(t *testing.T) {
		testLog := &testLogger{}
		logger := logr.New(testLog)
		err := errx.Operator("Simple error")

		logOperatorError(logger, err, "Simple error")

		assert.Len(t, testLog.errorCalls, 1, "should log exactly one error")
		call := testLog.errorCalls[0]
		kv := call.keysAndValues

		// Should have basic fields but no context fields
		assert.Contains(t, kv, "error.code", "should include error.code")
		assert.Contains(t, kv, "error.category", "should include error.category")
		assert.Contains(t, kv, "error.message", "should include error.message")
		assert.NotContains(t, kv, "error.context.mcpServer", "should not include context when not present")
	})
}

// getValue extracts a value from key-value pairs (logr format: key1, value1, key2, value2, ...)
func getValue(kv []interface{}, key string) interface{} {
	for i := 0; i < len(kv)-1; i += 2 {
		if kv[i] == key {
			return kv[i+1]
		}
	}
	return nil
}
