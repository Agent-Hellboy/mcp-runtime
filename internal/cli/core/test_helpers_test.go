package core

import (
	"bytes"
	"testing"
)

func setDefaultPrinterWriter(t *testing.T, w *bytes.Buffer) {
	t.Helper()
	orig := DefaultPrinter.Writer
	DefaultPrinter.Writer = w
	t.Cleanup(func() {
		DefaultPrinter.Writer = orig
	})
}
