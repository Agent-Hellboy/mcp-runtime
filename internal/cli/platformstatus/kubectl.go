package platformstatus

import (
	"fmt"
	"strings"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/kubeerr"
)

func runKubectlCombinedOutput(args []string) (string, error) {
	cmd, err := core.DefaultKubectlClient().CommandArgs(args)
	if err != nil {
		return "", err
	}
	output, execErr := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), execErr
}

// CheckClusterStatusQuiet probes cluster connectivity without printing status.
func CheckClusterStatusQuiet() error {
	output, err := runKubectlCombinedOutput([]string{"cluster-info"})
	if err == nil {
		return nil
	}
	detail := kubeerr.CommandDetail(output, err)
	if hint, handled := kubeerr.SetupHint(detail); handled {
		return core.WrapWithSentinel(core.ErrClusterNotAccessible, err, hint)
	}
	return core.WrapWithSentinel(core.ErrClusterNotAccessible, err, fmt.Sprintf("cluster not accessible: %s", detail))
}
