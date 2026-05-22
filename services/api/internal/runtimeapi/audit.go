package runtimeapi

import "context"

// SetAuditWriter overrides the audit sink used by runtime mutation handlers.
func (s *RuntimeServer) SetAuditWriter(writer auditWriter) {
	s.audit = writer
}

func (s *RuntimeServer) writeAudit(ctx context.Context, ev auditEvent) {
	if s == nil || s.audit == nil {
		return
	}
	s.audit.WriteAudit(ctx, ev)
}
