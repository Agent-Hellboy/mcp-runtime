package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Resolver maps platform user/team UUIDs to display labels.
type Resolver interface {
	ResolveUserIDs(ctx context.Context, ids []string) (map[string]string, error)
	ResolveTeamIDs(ctx context.Context, ids []string) (map[string]string, error)
}

type HTTPResolver struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func (r *HTTPResolver) ResolveUserIDs(ctx context.Context, ids []string) (map[string]string, error) {
	users, _, err := r.resolve(ctx, ids, nil)
	return users, err
}

func (r *HTTPResolver) ResolveTeamIDs(ctx context.Context, ids []string) (map[string]string, error) {
	_, teams, err := r.resolve(ctx, nil, ids)
	return teams, err
}

func (r *HTTPResolver) resolve(ctx context.Context, userIDs, teamIDs []string) (map[string]string, map[string]string, error) {
	if len(userIDs) == 0 && len(teamIDs) == 0 {
		return map[string]string{}, map[string]string{}, nil
	}
	body, err := json.Marshal(map[string]any{
		"user_ids": userIDs,
		"team_ids": teamIDs,
	})
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.BaseURL, "/")+"/internal/identity/resolve-ids", bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(r.Token))
	req.Header.Set("Content-Type", "application/json")
	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("identity resolve-ids: status %d", resp.StatusCode)
	}
	var result struct {
		Users map[string]string `json:"users"`
		Teams map[string]string `json:"teams"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, nil, err
	}
	if result.Users == nil {
		result.Users = map[string]string{}
	}
	if result.Teams == nil {
		result.Teams = map[string]string{}
	}
	return result.Users, result.Teams, nil
}
