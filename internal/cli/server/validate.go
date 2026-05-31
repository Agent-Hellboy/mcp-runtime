package server

// validateCmd implements `server validate` — checks .mcp/servers.yaml and optional
// grant/session YAML files for common errors before build/deploy so issues are
// caught early rather than at policy-enforcement time in the cluster.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/pkg/metadata"
)

type validateIssue struct {
	fatal   bool
	message string
	hint    string
}

func newValidateCmd() *cobra.Command {
	var metadataDir string
	var metadataFile string
	var grantFiles []string
	var sessionFiles []string
	var discoverURL string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate .mcp metadata and optional grant/session YAML files",
		Long: `Check .mcp/servers.yaml and optional MCPAccessGrant / MCPAgentSession YAML files
for common errors before building or deploying a server.

Catches:
  - missing or duplicate tool names
  - invalid trust levels and side-effect classes
  - grant tool rules that reference tools not in the metadata (causes
    tool_side_effect_unknown at the gateway)
  - grant allowedSideEffects that don't cover the tools they allow
  - session fields that are inconsistent with the server's policy

Use --from-server to also verify tool names against a locally running MCP server.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := loadMetadataForValidate(metadataDir, metadataFile)
			if err != nil {
				return err
			}
			core.Info(fmt.Sprintf("Validating metadata: %s", path))

			var issues []validateIssue

			for _, srv := range cfg.Servers {
				issues = append(issues, validateServer(srv)...)
			}

			for _, grantFile := range grantFiles {
				more, err := validateGrantFile(grantFile, cfg)
				if err != nil {
					issues = append(issues, validateIssue{fatal: true, message: fmt.Sprintf("cannot read grant file %s: %v", grantFile, err)})
					continue
				}
				issues = append(issues, more...)
			}

			for _, sessionFile := range sessionFiles {
				more, err := validateSessionFile(sessionFile, cfg)
				if err != nil {
					issues = append(issues, validateIssue{fatal: true, message: fmt.Sprintf("cannot read session file %s: %v", sessionFile, err)})
					continue
				}
				issues = append(issues, more...)
			}

			if discoverURL != "" {
				more := validateToolsAgainstServer(discoverURL, cfg)
				issues = append(issues, more...)
			}

			if len(issues) == 0 {
				core.Success("Validation passed — no issues found")
				return nil
			}

			hasFatal := false
			for _, issue := range issues {
				if issue.fatal {
					hasFatal = true
					core.Error(issue.message)
				} else {
					core.Warn(issue.message)
				}
				if issue.hint != "" {
					fmt.Printf("         Hint: %s\n", issue.hint)
				}
			}
			fmt.Println()
			if hasFatal {
				return fmt.Errorf("validation found %d issue(s) — fix them before deploying", len(issues))
			}
			fmt.Printf("Validation found %d warning(s)\n", len(issues))
			return nil
		},
	}

	cmd.Flags().StringVar(&metadataDir, "metadata-dir", ".mcp", "Directory containing servers.yaml")
	cmd.Flags().StringVar(&metadataFile, "metadata-file", "", "Explicit path to a servers.yaml file (overrides --metadata-dir)")
	cmd.Flags().StringArrayVar(&grantFiles, "grant-file", nil, "MCPAccessGrant YAML to validate against the server metadata; repeat for multiple files")
	cmd.Flags().StringArrayVar(&sessionFiles, "session-file", nil, "MCPAgentSession YAML to validate against the server metadata; repeat for multiple files")
	cmd.Flags().StringVar(&discoverURL, "from-server", "", "Verify tool names against a locally running MCP server at this URL")

	return cmd
}

// validateServer checks a single ServerConfig entry.
func validateServer(srv metadata.ServerMetadata) []validateIssue {
	var issues []validateIssue

	if strings.TrimSpace(srv.Name) == "" {
		issues = append(issues, validateIssue{fatal: true, message: "server entry has an empty name"})
	}

	seen := map[string]int{}
	for i, tool := range srv.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			issues = append(issues, validateIssue{
				fatal:   true,
				message: fmt.Sprintf("server %q: tool[%d] has an empty name", srv.Name, i),
				hint:    "Every tool must have a name that matches the tool's registered name in your MCP server code.",
			})
			continue
		}
		if prev, dup := seen[name]; dup {
			issues = append(issues, validateIssue{
				fatal:   true,
				message: fmt.Sprintf("server %q: duplicate tool name %q (entries %d and %d)", srv.Name, name, prev, i),
			})
		}
		seen[name] = i

		switch tool.SideEffect {
		case metadata.ToolSideEffectRead, metadata.ToolSideEffectWrite, metadata.ToolSideEffectDestructive:
		default:
			issues = append(issues, validateIssue{
				fatal:   true,
				message: fmt.Sprintf("server %q tool %q: invalid sideEffect %q", srv.Name, name, tool.SideEffect),
				hint:    "Valid values: read, write, destructive",
			})
		}

		switch tool.RequiredTrust {
		case metadata.TrustLevelLow, metadata.TrustLevelMedium, metadata.TrustLevelHigh, "":
		default:
			issues = append(issues, validateIssue{
				fatal:   true,
				message: fmt.Sprintf("server %q tool %q: invalid requiredTrust %q", srv.Name, name, tool.RequiredTrust),
				hint:    "Valid values: low, medium, high",
			})
		}
	}

	if len(srv.Tools) == 0 {
		issues = append(issues, validateIssue{
			fatal:   false,
			message: fmt.Sprintf("server %q: no tools defined — the gateway will deny all tool calls", srv.Name),
			hint:    "Run server init --from-server http://localhost:<port> to discover tool names from a running server.",
		})
	}

	return issues
}

// validateGrantFile reads an MCPAccessGrant YAML and checks that every tool
// rule references a tool that is defined in the server metadata.
func validateGrantFile(path string, cfg *metadata.RegistryFile) ([]validateIssue, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- explicit CLI flag path
	if err != nil {
		return nil, err
	}

	var grant struct {
		Spec struct {
			ServerRef struct {
				Name      string `yaml:"name" json:"name"`
				Namespace string `yaml:"namespace" json:"namespace"`
			} `yaml:"serverRef" json:"serverRef"`
			AllowedSideEffects []string `yaml:"allowedSideEffects" json:"allowedSideEffects"`
			ToolRules          []struct {
				Name     string `yaml:"name" json:"name"`
				Decision string `yaml:"decision" json:"decision"`
			} `yaml:"toolRules" json:"toolRules"`
		} `yaml:"spec" json:"spec"`
		Metadata struct {
			Name string `yaml:"name" json:"name"`
		} `yaml:"metadata" json:"metadata"`
	}
	if err := yaml.Unmarshal(data, &grant); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}

	grantName := grant.Metadata.Name
	serverName := grant.Spec.ServerRef.Name

	// Find the matching server in metadata
	var srv *metadata.ServerMetadata
	for i := range cfg.Servers {
		if cfg.Servers[i].Name == serverName {
			srv = &cfg.Servers[i]
			break
		}
	}

	var issues []validateIssue

	if srv == nil {
		issues = append(issues, validateIssue{
			fatal:   false,
			message: fmt.Sprintf("grant %q: serverRef.name %q not found in metadata (may be in a different metadata file)", grantName, serverName),
			hint:    "Pass --metadata-file pointing to the server's servers.yaml, or use the same --metadata-dir.",
		})
		return issues, nil
	}

	// Build tool map from server metadata
	toolMap := map[string]metadata.ToolConfig{}
	for _, t := range srv.Tools {
		toolMap[t.Name] = t
	}

	// Build allowed side effects set
	allowedEffects := map[string]struct{}{}
	for _, e := range grant.Spec.AllowedSideEffects {
		allowedEffects[strings.ToLower(e)] = struct{}{}
	}

	for _, rule := range grant.Spec.ToolRules {
		toolCfg, exists := toolMap[rule.Name]
		if !exists {
			issues = append(issues, validateIssue{
				fatal: true,
				message: fmt.Sprintf(
					"grant %q: toolRule %q references a tool not in server %q metadata — the gateway will return tool_side_effect_unknown",
					grantName, rule.Name, serverName,
				),
				hint: fmt.Sprintf(
					"Add %q to server %q tools in servers.yaml, or remove this rule from the grant. Known tools: %s",
					rule.Name, serverName, joinToolNames(srv.Tools),
				),
			})
			continue
		}

		if rule.Decision == "allow" {
			effect := string(toolCfg.SideEffect)
			if _, ok := allowedEffects[effect]; !ok {
				issues = append(issues, validateIssue{
					fatal: true,
					message: fmt.Sprintf(
						"grant %q: tool %q has sideEffect=%q but that side effect is not in allowedSideEffects %v — gateway will deny the call",
						grantName, rule.Name, effect, grant.Spec.AllowedSideEffects,
					),
					hint: fmt.Sprintf("Add %q to spec.allowedSideEffects in the grant YAML.", effect),
				})
			}
		}
	}

	return issues, nil
}

// validateSessionFile reads an MCPAgentSession YAML and checks basic consistency.
func validateSessionFile(path string, cfg *metadata.RegistryFile) ([]validateIssue, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- explicit CLI flag path
	if err != nil {
		return nil, err
	}

	var session struct {
		Spec struct {
			ServerRef struct {
				Name string `yaml:"name" json:"name"`
			} `yaml:"serverRef" json:"serverRef"`
			Subject struct {
				AgentID string `yaml:"agentID" json:"agentID"`
			} `yaml:"subject" json:"subject"`
			ConsentedTrust string `yaml:"consentedTrust" json:"consentedTrust"`
			ExpiresAt      string `yaml:"expiresAt" json:"expiresAt"`
		} `yaml:"spec" json:"spec"`
		Metadata struct {
			Name string `yaml:"name" json:"name"`
		} `yaml:"metadata" json:"metadata"`
	}
	if err := yaml.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}

	var issues []validateIssue
	name := session.Metadata.Name
	serverName := session.Spec.ServerRef.Name

	if strings.TrimSpace(session.Spec.Subject.AgentID) == "" {
		issues = append(issues, validateIssue{
			fatal:   true,
			message: fmt.Sprintf("session %q: spec.subject.agentID is empty", name),
			hint:    "Set --agent-id on session init, or fill spec.subject.agentID in the YAML.",
		})
	}

	switch strings.ToLower(session.Spec.ConsentedTrust) {
	case "low", "medium", "high", "":
	default:
		issues = append(issues, validateIssue{
			fatal:   true,
			message: fmt.Sprintf("session %q: invalid consentedTrust %q", name, session.Spec.ConsentedTrust),
			hint:    "Valid values: low, medium, high",
		})
	}

	// Check that the referenced server exists in metadata
	found := false
	for _, srv := range cfg.Servers {
		if srv.Name == serverName {
			found = true
			if srv.Policy.DefaultDecision == metadata.PolicyDecisionDeny && len(srv.Tools) == 0 {
				issues = append(issues, validateIssue{
					fatal:   false,
					message: fmt.Sprintf("session %q: server %q has default-deny policy but no tools — all tool calls will be denied", name, serverName),
				})
			}
			break
		}
	}

	if !found {
		issues = append(issues, validateIssue{
			fatal:   false,
			message: fmt.Sprintf("session %q: serverRef.name %q not found in metadata", name, serverName),
		})
	}

	return issues, nil
}

// validateToolsAgainstServer calls tools/list on a running MCP server and
// checks that each tool in the metadata actually exists there.
func validateToolsAgainstServer(url string, cfg *metadata.RegistryFile) []validateIssue {
	core.Info(fmt.Sprintf("Verifying tool names against running server: %s", url))
	discovered, err := DiscoverToolsFromServer(url)
	if err != nil {
		return []validateIssue{{
			fatal:   false,
			message: fmt.Sprintf("--from-server %s: %v", url, err),
			hint:    "Make sure the server is running and reachable before running validate.",
		}}
	}

	serverSet := map[string]struct{}{}
	for _, name := range discovered {
		serverSet[name] = struct{}{}
	}

	var issues []validateIssue
	for _, srv := range cfg.Servers {
		for _, tool := range srv.Tools {
			if _, ok := serverSet[tool.Name]; !ok {
				issues = append(issues, validateIssue{
					fatal: true,
					message: fmt.Sprintf(
						"server %q tool %q is in metadata but not found in tools/list from %s — gateway will return tool_side_effect_unknown",
						srv.Name, tool.Name, url,
					),
					hint: fmt.Sprintf("Server implements: %s", strings.Join(discovered, ", ")),
				})
			}
		}
		// Warn about tools on the server that are not in metadata
		metaSet := map[string]struct{}{}
		for _, t := range srv.Tools {
			metaSet[t.Name] = struct{}{}
		}
		for _, name := range discovered {
			if _, ok := metaSet[name]; !ok {
				issues = append(issues, validateIssue{
					fatal:   false,
					message: fmt.Sprintf("server %q: tool %q is exposed by the running server but not in metadata — it will be ungoverned (denied by default policy)", srv.Name, name),
					hint:    fmt.Sprintf("Add to servers.yaml: --tool %s  or  --tool-spec %s:low:read", name, name),
				})
			}
		}
	}
	return issues
}

func loadMetadataForValidate(dir, file string) (*metadata.RegistryFile, string, error) {
	if file != "" {
		cfg, err := metadata.LoadFromFile(file)
		if err != nil {
			return nil, "", err
		}
		return cfg, file, nil
	}
	path := filepath.Join(dir, "servers.yaml")
	cfg, err := metadata.LoadFromFile(path)
	if err != nil {
		return nil, "", err
	}
	return cfg, path, nil
}

func joinToolNames(tools []metadata.ToolConfig) string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return strings.Join(names, ", ")
}
