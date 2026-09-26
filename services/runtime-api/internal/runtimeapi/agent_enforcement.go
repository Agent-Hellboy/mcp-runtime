package runtimeapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

var errAgentDirectoryUnavailable = errors.New("agent directory is unavailable")
var errAgentNotActive = errors.New("agent is unknown, inactive, or belongs to another team")

const agentDirectoryEnforcementEnv = "MCP_AGENT_DIRECTORY_ENFORCEMENT"

// AgentDirectoryEnforcementMode returns the compatibility policy for agent
// IDs that have not yet been imported into the platform directory. The
// default remains warn so existing installations can migrate before opting
// into strict directory enforcement.
func AgentDirectoryEnforcementMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv(agentDirectoryEnforcementEnv)))
	switch mode {
	case "off", "warn", "enforce":
		return mode
	case "":
		return "warn"
	default:
		// An invalid operator setting must never weaken validation.
		return "enforce"
	}
}

// requireActiveAgent makes the platform directory authoritative anywhere a
// runtime identity containing an agent ID is accepted.
func requireActiveAgent(ctx context.Context, store identityStore, agentID, teamID string) error {
	agentID, teamID = strings.TrimSpace(agentID), strings.TrimSpace(teamID)
	if agentID == "" {
		return nil
	}
	mode := AgentDirectoryEnforcementMode()
	if store == nil || !store.Configured() {
		if mode == "warn" {
			log.Printf("agent directory warning: unable to verify agent %q for team %q: %v", agentID, teamID, errAgentDirectoryUnavailable)
		}
		if mode != "enforce" {
			return nil
		}
		return errAgentDirectoryUnavailable
	}
	agent, found, err := store.GetAgent(ctx, agentID)
	if err != nil {
		if mode == "warn" {
			log.Printf("agent directory warning: unable to verify agent %q for team %q: %v", agentID, teamID, err)
		}
		if mode != "enforce" {
			return nil
		}
		return fmt.Errorf("%w: %v", errAgentDirectoryUnavailable, err)
	}
	if !found {
		if mode == "warn" {
			log.Printf("agent directory warning: agent %q for team %q is not registered", agentID, teamID)
		}
		if mode != "enforce" {
			return nil
		}
		return errAgentNotActive
	}
	if agent.Status != "active" || teamID == "" || agent.TeamID != teamID {
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
