package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	policypkg "mcp-runtime/pkg/policy"
)

func (s *gatewayServer) startPolicyCache() error {
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

func (s *gatewayServer) reloadPolicy() error {
	doc, err := s.loadPolicy()
	if err != nil {
		current := s.loadPolicySnapshot()
		fallback := current.Policy
		if fallback == nil {
			fallback = s.defaultPolicyDocument()
		}
		s.snapshotPolicy(policySnapshot{Policy: fallback, Err: err})
		s.metrics.recordPolicyReload(s.metricScope(fallback), err)
		return err
	}
	s.snapshotPolicy(policySnapshot{Policy: doc})
	s.metrics.recordPolicyReload(s.metricScope(doc), nil)
	return nil
}

func (s *gatewayServer) currentPolicy() (*policypkg.Document, error) {
	snapshot := s.loadPolicySnapshot()
	if snapshot.Policy == nil {
		return s.defaultPolicyDocument(), snapshot.Err
	}
	return snapshot.Policy, snapshot.Err
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
	if s.policyFile != "" {
		data, err := os.ReadFile(s.policyFile)
		if err != nil {
			return nil, err
		} else if len(data) > 0 {
			if err := json.Unmarshal(data, doc); err != nil {
				return nil, err
			}
		}
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
	return doc, nil
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
	return &policypkg.Document{
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
}
