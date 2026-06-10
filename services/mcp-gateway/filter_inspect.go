package main

import "context"

// inspectFilter is stage 1 of the gateway pipeline. It performs bounded body
// capture and RPC inspection, setting Exchange.Inspection. All other fields
// remain at their zero values; this stage always returns Continue.
func (s *gatewayServer) inspectFilter(_ context.Context, ex *Exchange) Result {
	ex.Inspection = inspectRPCRequest(ex.R)
	return Continue
}
