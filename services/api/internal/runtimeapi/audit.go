package runtimeapi

import "context"

func (s *RuntimeServer) SetAuditWriter(writer auditWriter) {
	s.audit = writer
}

func (s *RuntimeServer) writeAudit(ctx context.Context, ev auditEvent) {
	if s == nil || s.audit == nil {
		return
	}
	s.audit.WriteAudit(ctx, ev)
}
