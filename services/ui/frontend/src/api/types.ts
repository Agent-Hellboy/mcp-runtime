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
