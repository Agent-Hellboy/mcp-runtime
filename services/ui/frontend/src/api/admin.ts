import { fetchJSON, withQuery } from "./client";
import type {
  AdminOperations,
  AuditLogEntry,
  ComponentStatus,
  GatewayEvent,
  GrantSummary,
  ImageActivity,
  SessionSummary,
  TeamRecord,
  UserActivity,
} from "./types";

// Every read here is a GET through the Phase 1 session proxy. Admin mutations
// are deliberately absent: the proxy is GET-only, and the direct /api/v1 write
// paths carry no browser credential (they answer 401), so a React write control
// would ship visibly broken. Mutations stay in the legacy fallback until a
// CSRF-protected mutating route exists.

function asArray<T>(value: unknown, key: string): T[] {
  if (!value || typeof value !== "object") {
    return [];
  }
  const items = (value as Record<string, unknown>)[key];
  return Array.isArray(items) ? (items as T[]) : [];
}

export async function listGrants(namespace?: string): Promise<GrantSummary[]> {
  return asArray<GrantSummary>(
    await fetchJSON(withQuery("/runtime/grants", { namespace })),
    "grants"
  );
}

export async function listSessions(namespace?: string): Promise<SessionSummary[]> {
  return asArray<SessionSummary>(
    await fetchJSON(withQuery("/runtime/sessions", { namespace })),
    "sessions"
  );
}

export async function listTeams(): Promise<TeamRecord[]> {
  return asArray<TeamRecord>(await fetchJSON("/runtime/teams"), "teams");
}

export async function listComponents(): Promise<ComponentStatus[]> {
  return asArray<ComponentStatus>(await fetchJSON("/runtime/components"), "components");
}

export async function readOperations(filters: {
  user?: string;
  since?: string;
  until?: string;
  limit?: string;
}): Promise<AdminOperations> {
  const payload = await fetchJSON(withQuery("/admin/operations", filters));
  return {
    users: asArray<UserActivity>(payload, "users"),
    audit_logs: asArray<AuditLogEntry>(payload, "audit_logs"),
    images: asArray<ImageActivity>(payload, "images"),
  };
}

// Gateway decision events power the grant/session drill-downs. This is the
// analytics path, which is unavailable on the contributor cluster.
export async function listEvents(params: {
  limit?: string;
  session_id?: string;
}): Promise<GatewayEvent[]> {
  return asArray<GatewayEvent>(await fetchJSON(withQuery("/events", params)), "events");
}
