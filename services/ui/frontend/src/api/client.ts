import { readRuntimeConfig } from "./config";

// Same-origin dashboard client. Auth is the HttpOnly mcp_ui_session cookie.
// This module does not read credentials from window, localStorage, or
// sessionStorage and never sends Authorization or x-api-key.

export const SESSION_PROXY_PREFIX = "/api/ui/v1";

export const SESSION_PROXY_GET_PATHS = new Set([
  "/runtime/namespaces",
  "/runtime/servers",
  "/runtime/tools",
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

export async function fetchJSON(path: string, options: RequestInit = {}): Promise<unknown> {
  const headers = new Headers(options.headers);
  headers.delete("Authorization");
  headers.delete("authorization");
  headers.delete("X-API-Key");
  headers.delete("x-api-key");

	const response = await fetch(apiURL(path, readRuntimeConfig().apiBase, options.method?.toString() || "GET"), {
    ...options,
    credentials: "same-origin",
    headers,
  });

  if (response.status === 401) {
    throw new UnauthorizedError();
  }
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `Request failed: ${response.status}`);
  }
  return response.json();
}
