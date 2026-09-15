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

export type UsageResponse = {
  totals: { events: number; allowed: number; denied: number; unique_servers: number; unique_humans: number; unique_agents: number };
  servers: Array<{ server: string; namespace: string; events: number; allowed: number; denied: number; unique_humans: number; unique_agents: number }>;
  actors: Array<{ human_id: string; agent_id: string; events: number; unique_servers: number; unique_tools: number; denied: number }>;
  tools: Array<{ server: string; tool_name: string; human_id: string; team_id: string; agent_id: string; events: number; denied: number }>;
  decisions: Array<{ decision: string; events: number }>;
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
