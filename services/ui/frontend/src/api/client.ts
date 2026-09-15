import { readRuntimeConfig } from "./config";

// Same-origin dashboard client. Auth is the HttpOnly mcp_ui_session cookie.
// This module does not read credentials from window, localStorage, or
// sessionStorage and never sends Authorization or x-api-key.

export const SESSION_PROXY_PREFIX = "/api/ui/v1";

export const SESSION_PROXY_GET_PATHS = new Set([
  "/runtime/namespaces",
  "/runtime/servers",
  "/runtime/tools",
  // Phase 4 admin reads. Each is already GET-allowlisted server-side in
  // sessionProxyRuntimePrefixes / sessionProxyAnalyticsPrefixes.
  "/runtime/grants",
  "/runtime/sessions",
  "/runtime/teams",
  "/runtime/components",
  "/admin/operations",
  "/admin/deployments",
  "/events",
]);

export class UnauthorizedError extends Error {
  readonly status = 401;

  constructor(message = "unauthorized") {
    super(message);
    this.name = "UnauthorizedError";
  }
}

export function apiURL(
  path: string,
  apiBase = readRuntimeConfig().apiBase,
  method = "GET"
): string {
  const normalized = path.startsWith("/") ? path : `/${path}`;
  const pathname = normalized.split("?")[0];
  if (method.toUpperCase() === "GET" && SESSION_PROXY_GET_PATHS.has(pathname)) {
    return `${SESSION_PROXY_PREFIX}${normalized}`;
  }
  const base = apiBase.replace(/\/$/, "") || "/api/v1";
  return `${base}${normalized}`;
}

// sameOriginInit builds a request that carries only the session cookie. Any
// caller-supplied credential header is dropped before it can reach the wire.
function sameOriginInit(options: RequestInit): RequestInit {
  const headers = new Headers(options.headers);
  headers.delete("Authorization");
  headers.delete("authorization");
  headers.delete("X-API-Key");
  headers.delete("x-api-key");

  return { ...options, credentials: "same-origin", headers };
}

async function readJSON(response: Response): Promise<unknown> {
  if (response.status === 401) {
    throw new UnauthorizedError();
  }
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `Request failed: ${response.status}`);
  }
  return response.json();
}

export async function fetchJSON(path: string, options: RequestInit = {}): Promise<unknown> {
  const url = apiURL(
    path,
    readRuntimeConfig().apiBase,
    options.method?.toString() || "GET"
  );
  return readJSON(await fetch(url, sameOriginInit(options)));
}

// Paths served by the UI origin itself (session lifecycle), not by the
// upstream runtime API. They intentionally bypass apiBase.
const UI_ORIGIN_PATHS = new Set(["/auth/status", "/auth/login", "/auth/logout"]);

export async function fetchUIJSON(path: string, options: RequestInit = {}): Promise<unknown> {
  if (!UI_ORIGIN_PATHS.has(path)) {
    throw new Error(`unsupported UI origin path: ${path}`);
  }
  return readJSON(await fetch(path, sameOriginInit(options)));
}

export function withQuery(path: string, params: Record<string, string | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    const trimmed = value?.trim();
    if (trimmed) {
      search.set(key, trimmed);
    }
  }
  const query = search.toString();
  return query ? `${path}?${query}` : path;
}
