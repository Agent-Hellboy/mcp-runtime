package team

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/platformapi"
)

type Manager struct {
	logger *zap.Logger
}

func NewManager(logger *zap.Logger) *Manager {
	return &Manager{logger: logger}
}

func (m *Manager) ListTeams() error {
	client, err := platformapi.NewPlatformClient()
	if err != nil {
		return err
	}
	teams, err := client.ListTeams(context.Background())
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "SLUG\tNAME\tNAMESPACE")
	for _, team := range teams {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", team.Slug, team.Name, team.Namespace)
	}
	_ = tw.Flush()
	return nil
}

func (m *Manager) CreateTeam(slug, name string) error {
	client, err := platformapi.NewPlatformClient()
	if err != nil {
		return err
	}
	slug = strings.TrimSpace(slug)
	name = strings.TrimSpace(name)
	if name == "" {
		name = slug
	}
	team, err := client.CreateTeam(context.Background(), slug, name)
	if err != nil {
		return err
	}
	core.Success(fmt.Sprintf("Created team %s (namespace: %s)", team.Slug, team.Namespace))
	return nil
}
