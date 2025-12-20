package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestPrintTable(t *testing.T) {
	data := [][]string{
		{"Name", "Age", "City"},
		{"John", "30", "New York"},
		{"Jane", "25", "Los Angeles"},
	}

	// Should not panic
	Table(data)
}

func TestPrintTableBoxed(t *testing.T) {
	data := [][]string{
		{"Server", "Status"},
		{"mcp-server-1", "Running"},
	}

	TableBoxed(data)
}

func TestPrintTableEmpty(t *testing.T) {
	// Empty table should not panic
	Table([][]string{})
	TableBoxed([][]string{})
}

func TestPrinterColors(t *testing.T) {
	// Color functions should return non-empty strings
	if Green("test") == "" {
		t.Error("Green should return non-empty string")
	}
	if Yellow("test") == "" {
		t.Error("Yellow should return non-empty string")
	}
	if Red("test") == "" {
		t.Error("Red should return non-empty string")
	}
	if Cyan("test") == "" {
		t.Error("Cyan should return non-empty string")
	}
}

func TestPrinterQuietMode(t *testing.T) {
	p := &Printer{Quiet: true}

	// These should not panic in quiet mode
	p.Section("test")
	p.Step("test")
	p.Info("test")
}

func TestPrinterSpinnerQuietMode(t *testing.T) {
	p := &Printer{Quiet: true}
	stop := p.SpinnerStart("working")
	stop(true, "done")
}

func TestPrinterPrintf(t *testing.T) {
	p := &Printer{}
	p.Printf("value=%d\n", 1)
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestPrinterSectionStepWithWriter(t *testing.T) {
	var buf bytes.Buffer
	p := &Printer{Writer: &buf}

	p.Section("Section Title")
	p.Step("Step Title")

	out := buf.String()
	if !strings.Contains(out, "Section Title") {
		t.Fatalf("expected section title in output, got %q", out)
	}
	if !strings.Contains(out, "Step Title") {
		t.Fatalf("expected step title in output, got %q", out)
	}
}

func TestPrinterStatusMessages(t *testing.T) {
	var buf bytes.Buffer
	p := &Printer{Writer: &buf}

	p.Info("info message")
	p.Success("ok message")
	p.Warn("warn message")
	p.Error("error message")

	out := buf.String()
	for _, msg := range []string{"info message", "ok message", "warn message", "error message"} {
		if !strings.Contains(out, msg) {
			t.Fatalf("expected %q in output, got %q", msg, out)
		}
	}

	p.Writer = nil
	p.Info("no writer info")
	p.Success("no writer ok")
	p.Warn("no writer warn")
	p.Error("no writer err")
}

func TestPrinterHeaderAndPlainOutput(t *testing.T) {
	var buf bytes.Buffer
	p := &Printer{Writer: &buf}

	p.Header("Header Title")
	p.Println("plain line")
	p.Printf("value=%d\n", 2)

	out := buf.String()
	if !strings.Contains(out, "Header Title") {
		t.Fatalf("expected header title in output, got %q", out)
	}
	if !strings.Contains(out, "plain line") {
		t.Fatalf("expected plain line in output, got %q", out)
	}
	if !strings.Contains(out, "value=2") {
		t.Fatalf("expected printf output in output, got %q", out)
	}

	p.Writer = nil
	p.Header("no writer header")
	p.Println("no writer line")
	p.Printf("value=%d\n", 3)
}

func TestPrinterSpinnerWithWriter(t *testing.T) {
	var buf bytes.Buffer
	p := &Printer{Writer: &buf}

	stop := p.SpinnerStart("working")
	stop(true, "done")

	stop = p.SpinnerStart("working")
	stop(false, "failed")
}

func TestPrinterTablesWithWriterError(t *testing.T) {
	p := &Printer{Writer: errWriter{}}
	data := [][]string{
		{"Name", "Value"},
		{"demo", "1"},
	}

	p.Table(data)
	p.TableBoxed(data)
}

func TestPrinterPackageLevelHelpers(t *testing.T) {
	var buf bytes.Buffer
	setDefaultPrinterWriter(t, &buf)

	Section("Section")
	Step("Step")
	Info("info")
	Success("success")
	Warn("warn")
	Error("error")
	Table([][]string{{"H1", "H2"}, {"A", "B"}})
	TableBoxed([][]string{{"H1", "H2"}, {"A", "B"}})
	Header("Header")

	stop := SpinnerStart("working")
	stop(true, "done")
}
