package platformstore

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

func (s *Store) ListUserActivity(ctx context.Context, filter OperationsFilter) ([]UserActivity, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	where, args := UserActivityWhere(filter)
	auditTimeWhere := AuditTimeWhere("a", filter, &args)
	args = append(args, limit)
	limitArg := len(args)
	query := `
SELECT u.id::text, u.email, u.role, COALESCE(n.namespace, ''), u.created_at,
       activity.last_login_at,
       activity.last_activity_at,
       COALESCE(activity.login_count, 0) AS login_count,
       COALESCE(activity.failed_action_count, 0) AS failed_action_count,
       COALESCE(registry.registry_credentials, 0) AS registry_credentials,
       COALESCE(keys.api_keys, 0) AS api_keys
FROM users u
LEFT JOIN namespaces n ON n.user_id = u.id AND n.deleted_at IS NULL
LEFT JOIN LATERAL (
	SELECT
		MAX(a.created_at) FILTER (WHERE a.action IN ('login', 'oidc_login') AND a.status = 'success') AS last_login_at,
		MAX(a.created_at) AS last_activity_at,
		COUNT(*) FILTER (WHERE a.action IN ('login', 'oidc_login') AND a.status = 'success') AS login_count,
		COUNT(*) FILTER (WHERE a.status IN ('denied', 'error')) AS failed_action_count
	FROM audit_logs a
	WHERE a.user_id = u.id` + auditTimeWhere + `
) activity ON true
LEFT JOIN LATERAL (
	SELECT COUNT(*) AS registry_credentials
	FROM registry_credentials rc
	WHERE rc.user_id = u.id AND rc.revoked = false
) registry ON true
LEFT JOIN LATERAL (
	SELECT COUNT(*) AS api_keys
	FROM api_keys ak
	WHERE ak.user_id = u.id AND ak.revoked = false
) keys ON true
` + where + `
ORDER BY COALESCE(activity.last_activity_at, u.created_at) DESC
LIMIT $` + strconv.Itoa(limitArg)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]UserActivity, 0, limit)
	for rows.Next() {
		var item UserActivity
		var lastLogin sql.NullTime
		var lastActivity sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.Email,
			&item.Role,
			&item.Namespace,
			&item.CreatedAt,
			&lastLogin,
			&lastActivity,
			&item.LoginCount,
			&item.FailedActionCount,
			&item.RegistryCredentials,
			&item.APIKeys,
		); err != nil {
			return nil, err
		}
		item.LastLoginAt = nullTimePtr(lastLogin)
		item.LastActivityAt = nullTimePtr(lastActivity)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListAuditLogs(ctx context.Context, filter OperationsFilter) ([]AuditLog, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	where, args := platformAuditWhere(filter)
	args = append(args, limit)
	limitArg := len(args)
	query := `
SELECT a.id, COALESCE(a.user_id::text, ''), COALESCE(u.email, ''), a.action, a.resource,
       COALESCE(a.namespace, ''), a.status, COALESCE(a.message, ''),
       COALESCE(a.actor_ip, ''), COALESCE(a.request_id, ''),
       COALESCE(a.source, ''), COALESCE(a.auth_identity, ''),
       COALESCE(a.image_ref, ''), COALESCE(a.server_name, ''),
       COALESCE(a.deployment_target, ''), a.created_at
FROM audit_logs a
LEFT JOIN users u ON u.id = a.user_id
` + where + `
ORDER BY a.created_at DESC, a.id DESC
LIMIT $` + strconv.Itoa(limitArg)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AuditLog, 0, limit)
	for rows.Next() {
		var item AuditLog
		if err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.Email,
			&item.Action,
			&item.Resource,
			&item.Namespace,
			&item.Status,
			&item.Message,
			&item.ActorIP,
			&item.RequestID,
			&item.Source,
			&item.AuthIdentity,
			&item.ImageRef,
			&item.ServerName,
			&item.DeploymentTarget,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListImageActivity(ctx context.Context, filter OperationsFilter) ([]ImageActivity, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	where, args := platformAuditWhere(filter)
	if where == "" {
		where = "WHERE a.image_ref IS NOT NULL AND a.image_ref <> ''"
	} else {
		where += " AND a.image_ref IS NOT NULL AND a.image_ref <> ''"
	}
	args = append(args, limit)
	limitArg := len(args)
	query := `
SELECT COALESCE(a.user_id::text, ''), COALESCE(u.email, ''), COALESCE(a.namespace, ''),
       a.image_ref, COALESCE(a.resource, ''), COALESCE(a.server_name, ''),
       COALESCE(a.deployment_target, ''), a.action, a.status,
       COALESCE(a.source, ''), a.created_at
FROM audit_logs a
LEFT JOIN users u ON u.id = a.user_id
` + where + `
ORDER BY a.created_at DESC, a.id DESC
LIMIT $` + strconv.Itoa(limitArg)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ImageActivity, 0, limit)
	for rows.Next() {
		var item ImageActivity
		if err := rows.Scan(
			&item.UserID,
			&item.Email,
			&item.Namespace,
			&item.ImageRef,
			&item.SourceImage,
			&item.ServerName,
			&item.DeploymentTarget,
			&item.Action,
			&item.Status,
			&item.Source,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) WriteAudit(ctx context.Context, ev AuditEvent) {
	if s == nil || s.db == nil {
		return
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO audit_logs (user_id,action,resource,namespace,status,message,actor_ip,request_id,source,auth_identity,image_ref,server_name,deployment_target) VALUES (NULLIF($1,'')::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		ev.UserID, ev.Action, ev.Resource, ev.Namespace, ev.Status, ev.Message, ev.ActorIP, ev.RequestID, ev.Source, ev.AuthIdentity, ev.ImageRef, ev.ServerName, ev.DeploymentTarget); err != nil {
		log.Printf("ERROR: failed to write audit log: %v", err)
	}
}

func UserActivityWhere(filter OperationsFilter) (string, []any) {
	conditions := []string{"u.deleted_at IS NULL"}
	args := make([]any, 0)
	if user := AdminOperationsUserSearch(filter); user != "" {
		pattern := "%" + user + "%"
		args = append(args, user, pattern, pattern)
		conditions = append(conditions, fmt.Sprintf("(u.id::text = $%d OR u.email ILIKE $%d OR COALESCE(n.namespace, '') ILIKE $%d)", len(args)-2, len(args)-1, len(args)))
	}
	return "WHERE " + strings.Join(conditions, " AND "), args
}

func platformAuditWhere(filter OperationsFilter) (string, []any) {
	conditions := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if user := AdminOperationsUserSearch(filter); user != "" {
		pattern := "%" + user + "%"
		args = append(args, user, pattern, pattern, pattern, pattern, pattern, pattern)
		conditions = append(conditions, fmt.Sprintf("(a.user_id::text = $%d OR COALESCE(u.email, '') ILIKE $%d OR COALESCE(a.namespace, '') ILIKE $%d OR COALESCE(a.resource, '') ILIKE $%d OR COALESCE(a.image_ref, '') ILIKE $%d OR COALESCE(a.server_name, '') ILIKE $%d OR COALESCE(a.deployment_target, '') ILIKE $%d)", len(args)-6, len(args)-5, len(args)-4, len(args)-3, len(args)-2, len(args)-1, len(args)))
	}
	if !filter.Since.IsZero() {
		args = append(args, filter.Since)
		conditions = append(conditions, fmt.Sprintf("a.created_at >= $%d", len(args)))
	}
	if !filter.Until.IsZero() {
		args = append(args, filter.Until)
		conditions = append(conditions, fmt.Sprintf("a.created_at <= $%d", len(args)))
	}
	if len(conditions) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conditions, " AND "), args
}

func AuditTimeWhere(alias string, filter OperationsFilter, args *[]any) string {
	conditions := make([]string, 0, 2)
	if !filter.Since.IsZero() {
		*args = append(*args, filter.Since)
		conditions = append(conditions, fmt.Sprintf("%s.created_at >= $%d", alias, len(*args)))
	}
	if !filter.Until.IsZero() {
		*args = append(*args, filter.Until)
		conditions = append(conditions, fmt.Sprintf("%s.created_at <= $%d", alias, len(*args)))
	}
	if len(conditions) == 0 {
		return ""
	}
	return " AND " + strings.Join(conditions, " AND ")
}

func AdminOperationsUserSearch(filter OperationsFilter) string {
	if filter.UserSearch != "" {
		return filter.UserSearch
	}
	return strings.ToLower(strings.TrimSpace(filter.User))
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}
