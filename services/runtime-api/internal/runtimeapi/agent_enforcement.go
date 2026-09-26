package runtimeapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var errAgentDirectoryUnavailable = errors.New("agent directory is unavailable")
var errAgentNotActive = errors.New("agent is unknown, inactive, or belongs to another team")

// requireActiveAgent makes the platform directory authoritative anywhere a
// runtime identity containing an agent ID is accepted.
func requireActiveAgent(ctx context.Context, store identityStore, agentID, teamID string) error {
	agentID, teamID = strings.TrimSpace(agentID), strings.TrimSpace(teamID)
	if agentID == "" {
		return nil
	}
	if store == nil || !store.Configured() {
		return errAgentDirectoryUnavailable
	}
	agent, found, err := store.GetAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("%w: %v", errAgentDirectoryUnavailable, err)
	}
	if !found || agent.Status != "active" || teamID == "" || agent.TeamID != teamID {
		return errAgentNotActive
	}
	return nil
}

func writeAgentDirectoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errAgentDirectoryUnavailable):
		writeAPIError(w, http.StatusServiceUnavailable, "agent directory is unavailable")
	case errors.Is(err, errAgentNotActive):
		writeAPIError(w, http.StatusUnprocessableEntity, "agent is unknown, inactive, or outside the subject team")
	default:
		writeAPIError(w, http.StatusInternalServerError, "agent directory check failed")
	}
}
