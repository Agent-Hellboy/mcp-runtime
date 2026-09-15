import { readRuntimeConfig } from "./config";

// Same-origin dashboard client. Auth is the HttpOnly mcp_ui_session cookie.
// This module does not read credentials from window, localStorage, or
// sessionStorage and never sends Authorization or x-api-key.

export const SESSION_PROXY_PREFIX = "/api/ui/v1";

export const SESSION_PROXY_GET_PATHS = new Set([
  "/runtime/namespaces",
  "/runtime/servers",
  "/runtime/tools",
  "/user/api-keys",
  "/user/analytics/usage",
  "/runtime/teams",
]);

// Paths the UI session may write through, mirroring the Go write allowlist in
// services/ui/session_proxy.go. A "/" suffix means one more path segment.
export const SESSION_PROXY_WRITE_PATHS: Array<{ path: string; methods: string[] }> = [
  { path: "/user/api-keys", methods: ["POST"] },
  { path: "/user/api-keys/", methods: ["DELETE"] },
];

export const CSRF_HEADER = "X-CSRF-Token";

// The CSRF token is session state held in memory only. It is deliberately not
// persisted: a reload re-reads it from /auth/status, and nothing durable on the
// device ever holds it.
let csrfToken = "";

export function setCSRFToken(token: string): void {
  csrfToken = typeof token === "string" ? token.trim() : "";
}

export function clearCSRFToken(): void {
  csrfToken = "";
}

// Exposed for tests and for surfacing a "reload to continue" state; callers
// must not render this value.
export function hasCSRFToken(): boolean {
  return csrfToken !== "";
}

export function isUnsafeMethod(method: string): boolean {
  return !["GET", "HEAD", "OPTIONS", "TRACE"].includes(method.toUpperCase());
}

function sessionProxyWriteAllowed(method: string, pathname: string): boolean {
  return SESSION_PROXY_WRITE_PATHS.some((route) => {
    if (!route.methods.includes(method.toUpperCase())) {
      return false;
    }
    if (route.path.endsWith("/")) {
      const rest = pathname.startsWith(route.path) ? pathname.slice(route.path.length) : "";
      return rest !== "" && !rest.includes("/");
    }
    return pathname === route.path;
  });
}

export class CSRFError extends Error {
  readonly status = 403;

  constructor(message = "csrf_failed") {
    super(message);
    this.name = "CSRFError";
  }
}

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
  if (sessionProxyWriteAllowed(method, pathname)) {
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
  // A caller must never choose its own CSRF token; only the session's token is
  // ever sent, and only on methods that can change state.
  headers.delete(CSRF_HEADER);
  headers.delete(CSRF_HEADER.toLowerCase());

  const method = options.method?.toString() || "GET";
  if (isUnsafeMethod(method) && csrfToken) {
    headers.set(CSRF_HEADER, csrfToken);
  }

  return { ...options, credentials: "same-origin", headers };
}

async function readJSON(response: Response): Promise<unknown> {
  if (response.status === 401) {
    throw new UnauthorizedError();
  }
  if (!response.ok) {
    const text = await response.text();
    if (response.status === 403 && text.includes("csrf_failed")) {
      throw new CSRFError();
    }
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
