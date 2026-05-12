package access

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	k8syaml "k8s.io/apimachinery/pkg/util/yaml"

	kubeapply "mcp-runtime/internal/cli/kube"
)

var emailShapeRegexp = regexp.MustCompile(`^[^@\s]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`)

type accessManifestMeta struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Subject struct {
			HumanID string `json:"humanID"`
			AgentID string `json:"agentID"`
		} `json:"subject"`
	} `json:"spec"`
}

func accessManifestWarningsFromFile(file string) ([]string, error) {
	absPath, err := kubeapply.ResolveRegularFilePath(file)
	if err != nil {
		return nil, err
	}
	data, err := kubeapply.ReadFileAtPath(absPath)
	if err != nil {
		return nil, err
	}
	return accessManifestWarnings(data)
}

func accessManifestWarnings(data []byte) ([]string, error) {
	decoder := k8syaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	docIndex := 0
	var warnings []string
	for {
		var rawDoc map[string]any
		if err := decoder.Decode(&rawDoc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return warnings, err
		}
		if len(rawDoc) == 0 {
			continue
		}
		docIndex++
		raw, err := json.Marshal(rawDoc)
		if err != nil {
			return warnings, err
		}
		var meta accessManifestMeta
		if err := json.Unmarshal(raw, &meta); err != nil {
			return warnings, err
		}
		switch strings.TrimSpace(meta.Kind) {
		case "MCPAccessGrant", "MCPAgentSession":
			warnings = append(warnings, humanIDWarnings(docIndex, meta)...)
		}
	}
	return warnings, nil
}

func humanIDWarnings(docIndex int, meta accessManifestMeta) []string {
	humanID := strings.TrimSpace(meta.Spec.Subject.HumanID)
	if humanID == "" {
		return nil
	}
	prefix := fmt.Sprintf("document %d %s %s/%s subject.humanID %q", docIndex, meta.Kind, namespaceOrDefault(meta.Metadata.Namespace), nameOrUnknown(meta.Metadata.Name), humanID)
	var warnings []string
	if humanID != meta.Spec.Subject.HumanID || strings.ContainsAny(humanID, " \t\r\n") {
		warnings = append(warnings, prefix+": contains whitespace; gateway matching is exact")
	}
	if strings.Contains(humanID, "@") {
		if !emailShapeRegexp.MatchString(humanID) {
			warnings = append(warnings, prefix+": looks like an email identifier but does not match the recommended email shape")
		}
		if humanID != strings.ToLower(humanID) {
			warnings = append(warnings, prefix+": contains uppercase characters; gateway matching is case-sensitive")
		}
	}
	if strings.Contains(strings.ToLower(humanID), "mcp-team-") {
		warnings = append(warnings, prefix+": appears to encode a team namespace; prefer a stable identity-provider subject or email")
	}
	return warnings
}

func namespaceOrDefault(namespace string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return "mcp-servers"
	}
	return namespace
}

func nameOrUnknown(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "<unnamed>"
	}
	return name
}
