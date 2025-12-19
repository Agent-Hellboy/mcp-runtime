// Package cli provides terminal output utilities for the mcp-runtime CLI.
// All terminal formatting is centralized here to abstract the underlying library (pterm).
package cli

import (
	"github.com/pterm/pterm"
)

// Printer provides formatted terminal output methods.
// Use the default instance via package-level functions.
type Printer struct {
	// Quiet suppresses non-essential output
	Quiet bool
}

// DefaultPrinter is the default printer instance used by package-level functions.
var DefaultPrinter = &Printer{}

// --- Section & Step Headers ---

// Section prints a prominent section header.
func (p *Printer) Section(title string) {
	if p.Quiet {
		return
	}
	pterm.Println()
	pterm.DefaultSection.Println(title)
}

// Step prints a step indicator (e.g., "Step 1: Initialize").
func (p *Printer) Step(title string) {
	if p.Quiet {
		return
	}
	pterm.Println()
	pterm.DefaultSection.WithLevel(2).Println(title)
}

// --- Status Messages ---

// Info prints an informational message.
func (p *Printer) Info(msg string) {
	if p.Quiet {
		return
	}
	pterm.Info.Println(msg)
}

// Success prints a success message.
func (p *Printer) Success(msg string) {
	pterm.Success.Println(msg)
}

// Warn prints a warning message.
func (p *Printer) Warn(msg string) {
	pterm.Warning.Println(msg)
}

// Error prints an error message.
func (p *Printer) Error(msg string) {
	pterm.Error.Println(msg)
}

// --- Tables ---

// Table prints a formatted table. First row is treated as header.
func (p *Printer) Table(data [][]string) {
	if len(data) == 0 {
		return
	}
	if err := pterm.DefaultTable.WithHasHeader().WithData(data).Render(); err != nil {
		pterm.Error.Println("failed to render table:", err)
	}
}

// TableBoxed prints a formatted table with box borders.
func (p *Printer) TableBoxed(data [][]string) {
	if len(data) == 0 {
		return
	}
	if err := pterm.DefaultTable.WithHasHeader().WithBoxed().WithData(data).Render(); err != nil {
		pterm.Error.Println("failed to render table:", err)
	}
}

// --- Headers & Banners ---

// Header prints a full-width header banner.
func (p *Printer) Header(title string) {
	pterm.DefaultHeader.WithFullWidth().WithBackgroundStyle(pterm.NewStyle(pterm.BgCyan)).Println(title)
}

// --- Colors ---

// Green returns green-colored text.
func (p *Printer) Green(msg string) string {
	return pterm.Green(msg)
}

// Yellow returns yellow-colored text.
func (p *Printer) Yellow(msg string) string {
	return pterm.Yellow(msg)
}

// Red returns red-colored text.
func (p *Printer) Red(msg string) string {
	return pterm.Red(msg)
}

// Cyan returns cyan-colored text.
func (p *Printer) Cyan(msg string) string {
	return pterm.Cyan(msg)
}

// --- Spinners ---

// SpinnerStart starts a spinner with the given message. Returns a stop function.
func (p *Printer) SpinnerStart(msg string) func(success bool, finalMsg string) {
	if p.Quiet {
		return func(bool, string) {}
	}
	spinner, _ := pterm.DefaultSpinner.Start(msg)
	return func(success bool, finalMsg string) {
		if success {
			spinner.Success(finalMsg)
		} else {
			spinner.Fail(finalMsg)
		}
	}
}

// --- Plain Output ---

// Println prints a plain line.
func (p *Printer) Println(a ...interface{}) {
	pterm.Println(a...)
}

// Printf prints formatted text.
func (p *Printer) Printf(format string, a ...interface{}) {
	pterm.Printf(format, a...)
}

// --- Package-level convenience functions (use DefaultPrinter) ---

// Section prints a section header.
func Section(title string) { DefaultPrinter.Section(title) }

// Step prints a step header.
func Step(title string) { DefaultPrinter.Step(title) }

// Info prints an info message.
func Info(msg string) { DefaultPrinter.Info(msg) }

// Success prints a success message.
func Success(msg string) { DefaultPrinter.Success(msg) }

// Warn prints a warning message.
func Warn(msg string) { DefaultPrinter.Warn(msg) }

// Error prints an error message.
func Error(msg string) { DefaultPrinter.Error(msg) }

// Table prints a table.
func Table(data [][]string) { DefaultPrinter.Table(data) }

// TableBoxed prints a boxed table.
func TableBoxed(data [][]string) { DefaultPrinter.TableBoxed(data) }

// Header prints a header banner.
func Header(title string) { DefaultPrinter.Header(title) }

// Green returns green text.
func Green(msg string) string { return DefaultPrinter.Green(msg) }

// Yellow returns yellow text.
func Yellow(msg string) string { return DefaultPrinter.Yellow(msg) }

// Red returns red text.
func Red(msg string) string { return DefaultPrinter.Red(msg) }

// Cyan returns cyan text.
func Cyan(msg string) string { return DefaultPrinter.Cyan(msg) }

// SpinnerStart starts a spinner.
func SpinnerStart(msg string) func(success bool, finalMsg string) {
	return DefaultPrinter.SpinnerStart(msg)
}
