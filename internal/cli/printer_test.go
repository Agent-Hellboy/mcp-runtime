package cli

import (
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
