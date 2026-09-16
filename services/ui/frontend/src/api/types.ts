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

// GET /runtime/servers's publish_policy field
// (services/runtime-api/internal/runtimeapi/servers.go). Only meaningful for
// a non-admin principal - the runtime does not cap admin publishing.
export type PublishPolicy = {
  active_server_limit_enabled?: boolean;
  active_server_count?: number;
  active_server_limit?: number;
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

// Mirrors legacy's formatPublishQuota() (services/ui/static/legacy/app.js):
// "off" when the limit isn't enforced, otherwise "count/limit". Callers
// gate visibility themselves - the runtime only enforces this for non-admin
// principals, so it is only meaningful (and only ever legacy-gated visible)
// for a tenant user.
export function formatPublishQuota(policy: PublishPolicy | null | undefined): string {
  if (!policy || policy.active_server_limit_enabled !== true) {
    return "off";
  }
  const limit = Number(policy.active_server_limit || 0);
  if (!limit) {
    return "off";
  }
  const count = Number(policy.active_server_count || 0);
  return `${count}/${limit}`;
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

// --- Phase 4: admin governance and operations -------------------------------

export type SubjectRef = {
  humanID?: string;
  agentID?: string;
  teamID?: string;
};

export type ServerRef = {
  name?: string;
  namespace?: string;
};

export type GrantSummary = {
  name: string;
  namespace: string;
  serverRef?: ServerRef;
  subject?: SubjectRef;
  maxTrust?: string;
  allowedSideEffects?: string[];
  disabled: boolean;
  age?: string;
};

export type SessionSummary = {
  name: string;
  namespace: string;
  serverRef?: ServerRef;
  subject?: SubjectRef;
  consentedTrust?: string;
  revoked: boolean;
  expiresAt?: string;
  age?: string;
};

export type TeamRecord = {
  id: string;
  slug: string;
  name: string;
  namespace: string;
  created_at?: string;
};

export type TeamMembership = {
  team_id?: string;
  team_slug?: string;
  team_name?: string;
  team_namespace?: string;
  user_id: string;
  email?: string;
  role: string;
  created_at?: string;
  id?: string;
  slug?: string;
  name?: string;
  namespace?: string;
};

export type ComponentStatus = {
  key: string;
  display: string;
  namespace: string;
  kind: string;
  resource: string;
  status: string;
  ready: string;
  message?: string;
};

export type UserActivity = {
  id: string;
  email: string;
  role: string;
  namespace?: string;
  last_login_at?: string;
  last_activity_at?: string;
  login_count: number;
  failed_action_count: number;
  registry_credentials: number;
  api_keys: number;
};

export type AuditLogEntry = {
  user_id?: string;
  action: string;
  resource: string;
  namespace?: string;
  status: string;
  message?: string;
  actor_ip?: string;
  source?: string;
  auth_identity?: string;
  image_ref?: string;
  server_name?: string;
  created_at?: string;
};

export type ImageActivity = {
  email?: string;
  namespace?: string;
  image_ref: string;
  server_name?: string;
  deployment_target?: string;
  action: string;
  status: string;
  created_at?: string;
};

export type AdminOperations = {
  users: UserActivity[];
  audit_logs: AuditLogEntry[];
  images: ImageActivity[];
};

export type GatewayEvent = {
  timestamp?: string;
  namespace?: string;
  tool_name?: string;
  decision?: string;
  source?: string;
  event_type?: string;
  payload?: Record<string, unknown>;
};

// The authenticated principal returned by the server is the only source of
// truth for admin access. Anything else must fail closed.
export function isAdmin(status: AuthStatus): boolean {
  return status.authenticated === true && status.principal?.role === "admin";
}

export function subjectLabel(subject: SubjectRef | undefined): string {
  if (!subject) {
    return "—";
  }
  return [subject.humanID, subject.agentID, subject.teamID].filter(Boolean).join(" / ") || "—";
}

export function accessKey(item: { name: string; namespace: string }): string {
  return `${item.namespace}/${item.name}`;
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
  unique_sessions?: number;
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
  last_seen?: string;
};

export type ToolUsage = {
  server: string;
  tool_name: string;
  human_id: string;
  team_id: string;
  agent_id: string;
  events: number;
  denied: number;
  last_seen?: string;
};

export type UsageResponse = {
  totals: UsageTotals;
  servers: ServerUsage[];
  tools: ToolUsage[];
  window_days?: number;
  actors?: Array<{ human_id: string; agent_id: string; events: number; unique_servers: number; unique_tools: number; denied: number }>;
  decisions?: Array<{ decision: string; events: number }>;
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
