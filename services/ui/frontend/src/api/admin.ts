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
  UsageResponse,
} from "./types";

// Reads and writes go through the same-origin session proxy. The proxy owns the
// upstream credential and requires its in-memory CSRF token for every write.

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

function segment(value: string): string {
  return encodeURIComponent(value);
}

function jsonBody(value: unknown): RequestInit {
  return {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(value),
  };
}

export async function setGrantDisabled(
  namespace: string,
  name: string,
  disabled: boolean
): Promise<void> {
  await fetchJSON(`/runtime/grants/${segment(namespace)}/${segment(name)}`, {
    method: "PATCH",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ disabled }),
  });
}

export async function setSessionRevoked(
  namespace: string,
  name: string,
  revoked: boolean
): Promise<void> {
  await fetchJSON(`/runtime/sessions/${segment(namespace)}/${segment(name)}`, {
    method: "PATCH",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ revoked }),
  });
}

export async function createGrant(input: {
  name: string;
  namespace: string;
  serverRef: { name: string; namespace?: string };
  subject: { humanID?: string; agentID?: string; teamID?: string };
  maxTrust: string;
  allowedSideEffects: string[];
}): Promise<void> {
  await fetchJSON("/runtime/grants", jsonBody({ ...input, policyVersion: "", toolRules: [] }));
}

export async function createSession(input: {
  name: string;
  namespace: string;
  serverRef: { name: string; namespace?: string };
  subject: { humanID?: string; agentID?: string; teamID?: string };
  consentedTrust: string;
  expiresAt?: string;
}): Promise<void> {
  await fetchJSON("/runtime/sessions", jsonBody({ ...input, policyVersion: "" }));
}

export async function createTeam(slug: string, name: string): Promise<void> {
  await fetchJSON("/runtime/teams", jsonBody({ slug, name }));
}

export async function deleteTeam(slug: string): Promise<void> {
  await fetchJSON(`/runtime/teams/${segment(slug)}`, { method: "DELETE" });
}

export async function listTeamMembers(slug: string): Promise<import("./types").TeamMembership[]> {
  return asArray<import("./types").TeamMembership>(
    await fetchJSON(`/runtime/teams/${segment(slug)}/members`),
    "members"
  );
}

export async function createTeamUser(
  slug: string,
  email: string,
  password: string,
  role: string
): Promise<void> {
  await fetchJSON(`/runtime/teams/${segment(slug)}/users`, jsonBody({ email, password, role }));
}

export async function setTeamMemberRole(slug: string, userID: string, role: string): Promise<void> {
  await fetchJSON(`/runtime/teams/${segment(slug)}/members/${segment(userID)}`, {
    method: "PUT",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ role }),
  });
}

export async function removeTeamMember(slug: string, userID: string): Promise<void> {
  await fetchJSON(`/runtime/teams/${segment(slug)}/members/${segment(userID)}`, {
    method: "DELETE",
  });
}

export async function restartComponent(component?: string): Promise<void> {
  await fetchJSON("/runtime/actions/restart", jsonBody(component ? { component } : { all: true }));
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

export async function listUsage(limit = "10"): Promise<UsageResponse> {
  const value = await fetchJSON(withQuery("/analytics/usage", { limit }));
  const payload = value as Partial<UsageResponse>;
  return {
    totals: payload.totals || { events: 0, allowed: 0, denied: 0, unique_servers: 0, unique_humans: 0, unique_agents: 0 },
    servers: payload.servers || [],
    actors: payload.actors || [],
    tools: payload.tools || [],
    decisions: payload.decisions || [],
  };
}
