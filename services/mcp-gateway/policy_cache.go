package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	policypkg "mcp-runtime/pkg/policy"
)

// errPolicyUnavailable is returned by currentPolicy when no validated policy
// snapshot has been activated yet. Callers fail tool calls closed in this state.
var errPolicyUnavailable = errors.New("policy_unavailable")

func (s *gatewayServer) startPolicyCache() error {
	// Seed with the default document so reads never observe a nil policy. It is
	// not marked Ready: in file-backed mode the gateway is only ready once the
	// rendered policy validates and activates below.
	s.snapshotPolicy(policySnapshot{Policy: s.defaultPolicyDocument()})
	if err := s.reloadPolicy(); err != nil {
		return err
	}
	if s.policyFile == "" {
		return nil
	}

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := s.reloadPolicy(); err != nil {
				log.Printf("policy reload failed: %v", err)
			}
		}
	}()

	return nil
}

// reloadPolicy loads, validates, and atomically activates the policy. A load or
// validation failure never replaces the last-known-good snapshot: the previous
// Policy/Revision/LoadedAt/Ready are retained and only Err is updated so that
// /config/status and metrics surface the failure while traffic keeps flowing.
func (s *gatewayServer) reloadPolicy() error {
	doc, err := s.loadPolicy()
	if err == nil {
		err = policypkg.Validate(doc)
	}
	if err != nil {
		retained := s.loadPolicySnapshot()
		retained.Err = err
		s.snapshotPolicy(retained)
		recordPolicyReloadFailure()
		return err
	}

	loadedAt := time.Now()
	s.snapshotPolicy(policySnapshot{
		Policy:   doc,
		Revision: doc.Revision,
		LoadedAt: loadedAt,
		Ready:    true,
	})
	recordPolicyReloadSuccess(doc.Revision, doc.SchemaVersion, loadedAt)
	return nil
}

func (s *gatewayServer) currentPolicy() (*policypkg.Document, error) {
	snapshot := s.loadPolicySnapshot()
	if snapshot.Policy == nil {
		return s.defaultPolicyDocument(), errPolicyUnavailable
	}
	// Until the first valid policy is activated, fail tool calls closed even
	// though a seed document is present. Once Ready, a later failed reload is
	// surfaced via /config/status but traffic keeps using last-known-good.
	if !snapshot.Ready {
		return snapshot.Policy, errPolicyUnavailable
	}
	return snapshot.Policy, nil
}

func (s *gatewayServer) loadPolicySnapshot() policySnapshot {
	if value := s.policyState.Load(); value != nil {
		return value.(policySnapshot)
	}
	return policySnapshot{Policy: s.defaultPolicyDocument()}
}

func (s *gatewayServer) snapshotPolicy(snapshot policySnapshot) {
	s.policyState.Store(snapshot)
}

func (s *gatewayServer) loadPolicy() (*policypkg.Document, error) {
	doc := &policypkg.Document{}
	fromFile := false
	if s.policyFile != "" {
		data, err := os.ReadFile(s.policyFile)
		if err != nil {
			return nil, err
		} else if len(data) > 0 {
			if err := json.Unmarshal(data, doc); err != nil {
				return nil, err
			}
			fromFile = true
		}
	}

	s.applyPolicyDefaults(doc)

	// A file-backed document carries the operator-stamped schema version and
	// revision and is reported verbatim. A gateway-generated default document
	// (no policy file, or an empty one) is stamped here so it carries a
	// supported schema version and deterministic revision and can validate.
	if !fromFile {
		if err := policypkg.Stamp(doc, ""); err != nil {
			return nil, err
		}
	}
	return doc, nil
}

// applyPolicyDefaults fills server identity and auth/policy defaults on a
// decoded or empty document, initializing the Auth and Policy sub-documents
// when absent so callers never dereference a nil pointer.
func (s *gatewayServer) applyPolicyDefaults(doc *policypkg.Document) {
	if doc.Auth == nil {
		doc.Auth = &policypkg.Auth{}
	}
	if doc.Policy == nil {
		doc.Policy = &policypkg.Config{}
	}
	if doc.Server.Name == "" {
		doc.Server.Name = policypkg.ServerName(s.serverName)
	}
	if doc.Server.Namespace == "" {
		doc.Server.Namespace = policypkg.Namespace(s.serverNamespace)
	}
	if doc.Server.Cluster == "" {
		doc.Server.Cluster = s.clusterName
	}
	if doc.Auth.Mode == "" {
		doc.Auth.Mode = "header"
	}
	if doc.Auth.HumanIDHeader == "" {
		doc.Auth.HumanIDHeader = s.defaultHumanHeader
	}
	if doc.Auth.AgentIDHeader == "" {
		doc.Auth.AgentIDHeader = s.defaultAgentHeader
	}
	if doc.Auth.TeamIDHeader == "" {
		doc.Auth.TeamIDHeader = s.defaultTeamHeader
	}
	if doc.Auth.SessionIDHeader == "" {
		doc.Auth.SessionIDHeader = s.defaultSessionHeader
	}
	if strings.EqualFold(doc.Auth.Mode, "oauth") && doc.Auth.TokenHeader == "" {
		doc.Auth.TokenHeader = defaultTokenHeader
	}
	if doc.Policy.Mode == "" {
		doc.Policy.Mode = s.defaultPolicyMode
	}
	if doc.Policy.DefaultDecision == "" {
		doc.Policy.DefaultDecision = s.defaultPolicyDecision
	}
	if doc.Policy.PolicyVersion == "" {
		doc.Policy.PolicyVersion = s.defaultPolicyVersion
	}
}

func (s *gatewayServer) extractIdentity(r *http.Request, policy *policypkg.Document) identityContext {
	humanHeader, agentHeader, teamHeader, sessionHeader := s.identityHeaderNames(policy)
	return identityContext{
		HumanID:   strings.TrimSpace(r.Header.Get(humanHeader)),
		AgentID:   strings.TrimSpace(r.Header.Get(agentHeader)),
		TeamID:    strings.TrimSpace(r.Header.Get(teamHeader)),
		SessionID: strings.TrimSpace(r.Header.Get(sessionHeader)),
	}
}

func policyIdentity(identity identityContext) policypkg.Identity {
	return policypkg.Identity{
		HumanID:   policypkg.HumanID(identity.HumanID),
		AgentID:   policypkg.AgentID(identity.AgentID),
		TeamID:    policypkg.TeamID(identity.TeamID),
		SessionID: policypkg.SessionID(identity.SessionID),
	}
}

func (s *gatewayServer) defaultPolicyDocument() *policypkg.Document {
	doc := &policypkg.Document{
		Server: policypkg.Server{
			Name:      policypkg.ServerName(s.serverName),
			Namespace: policypkg.Namespace(s.serverNamespace),
			Cluster:   s.clusterName,
		},
		Auth: &policypkg.Auth{
			Mode:            "header",
			HumanIDHeader:   s.defaultHumanHeader,
			AgentIDHeader:   s.defaultAgentHeader,
			TeamIDHeader:    s.defaultTeamHeader,
			SessionIDHeader: s.defaultSessionHeader,
			TokenHeader:     defaultTokenHeader,
		},
		Policy: &policypkg.Config{
			Mode:            s.defaultPolicyMode,
			DefaultDecision: s.defaultPolicyDecision,
			PolicyVersion:   s.defaultPolicyVersion,
		},
	}
	// Stamp so the default carries a supported schema version and deterministic
	// revision; this is best-effort and the document is well-formed by
	// construction, so any error is non-fatal.
	_ = policypkg.Stamp(doc, "")
	return doc
}
