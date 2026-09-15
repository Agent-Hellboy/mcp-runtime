// Response shapes for the migrated Servers workspace. Field names mirror the
// runtime API JSON tags in services/runtime-api/internal/runtimeapi.

export type NamespaceEntry = {
  namespace: string;
  is_shared?: boolean;
  is_public?: boolean;
  scope?: string;
  scope_name?: string;
  team_id?: string;
  team_name?: string;
  team_slug?: string;
};

export type ServerSummary = {
  name: string;
  namespace: string;
  uid?: string;
  team_id?: string;
  image?: string;
  description?: string;
  ready: string;
  status: string;
  age?: string;
  endpoint?: string;
  authMode?: string;
  tools?: Array<{ name?: string }>;
};

export type ToolRow = {
  tool_name: string;
  description?: string;
  server_name: string;
  namespace: string;
  team_id?: string;
  endpoint_url?: string;
  declared: boolean;
  live: boolean;
  drift_status: string;
  required_trust?: string;
  side_effect?: string;
  risk_level?: string;
  labels?: Record<string, string>;
};

export type Principal = {
  role?: string;
  subject?: string;
  email?: string;
  auth_type?: string;
};

export type AuthStatus = {
  authenticated: boolean;
  principal?: Principal;
};

export function serverKey(server: Pick<ServerSummary, "name" | "namespace">): string {
  return `${server.namespace}/${server.name}`;
}

export function toolKey(tool: ToolRow): string {
  return `${tool.namespace}/${tool.server_name}/${tool.tool_name}`;
}

// A server is "ready" when its readiness string reports every replica up.
export function isServerReady(server: ServerSummary): boolean {
  const ready = (server.ready || "").trim();
  const [current, desired] = ready.split("/");
  if (!desired) {
    return ready.toLowerCase() === "true";
  }
  return current === desired && Number(desired) > 0;
}


// --- Phase 3: user workflows -------------------------------------------------

// Mirrors platformclient.APIKeySummary. The raw key value is deliberately not
// part of this type: it exists only in the one-time create response.
export type UserAPIKey = {
  id: string;
  name: string;
  prefix: string;
  created_at: string;
  revoked: boolean;
  revoked_at?: string;
};

export type UsageTotals = {
  events: number;
  allowed: number;
  denied: number;
  unique_servers: number;
  unique_humans: number;
  unique_agents: number;
  unique_sessions: number;
};

export type ServerUsage = {
  server: string;
  namespace: string;
  team_id?: string;
  events: number;
  allowed: number;
  denied: number;
  unique_humans: number;
  unique_agents: number;
  last_seen: string;
};

export type ToolUsage = {
  server: string;
  tool_name: string;
  human_id: string;
  team_id: string;
  agent_id: string;
  events: number;
  denied: number;
  last_seen: string;
};

export type UsageResponse = {
  totals: UsageTotals;
  servers: ServerUsage[];
  tools: ToolUsage[];
  window_days: number;
};

export type TeamMembership = {
  id?: string;
  slug?: string;
  name?: string;
  namespace?: string;
  role?: string;
};

// Legacy role gating, reproduced exactly (services/ui/static/legacy/app.js).
// Activity is tenant-only; API keys additionally require a user identity.
export function isAdminPrincipal(status: AuthStatus): boolean {
  return status.authenticated && status.principal?.role === "admin";
}

export function isTenantUser(status: AuthStatus): boolean {
  return status.authenticated && !isAdminPrincipal(status);
}

export function hasUserIdentity(status: AuthStatus): boolean {
  return status.authenticated && (status.principal?.subject || "").trim() !== "";
}
