import { UnauthorizedError, clearCSRFToken, fetchUIJSON, setCSRFToken } from "./client";
import type { AuthStatus } from "./types";

function asAuthStatus(value: unknown): AuthStatus {
  if (!value || typeof value !== "object") {
    return { authenticated: false };
  }
  const record = value as Record<string, unknown>;
  const authenticated = record.authenticated === true;
  // The CSRF token rides the auth response and is held in memory by the client
  // module; it is never returned to callers or rendered.
  if (authenticated && typeof record.csrf_token === "string") {
    setCSRFToken(record.csrf_token);
  } else if (!authenticated) {
    clearCSRFToken();
  }
  return {
    authenticated,
    principal: (record.principal as AuthStatus["principal"]) || undefined,
  };
}

export async function readAuthStatus(): Promise<AuthStatus> {
  try {
    return asAuthStatus(await fetchUIJSON("/auth/status"));
  } catch (err) {
    if (err instanceof UnauthorizedError) {
      return { authenticated: false };
    }
    throw err;
  }
}

export type LoginInput =
  | { email: string; password: string }
  | { apiKey: string }
  | { idToken: string };

export async function login(input: LoginInput): Promise<AuthStatus> {
  const body =
    "apiKey" in input
      ? { api_key: input.apiKey }
      : "idToken" in input
        ? { id_token: input.idToken }
        : { email: input.email, password: input.password };
  return asAuthStatus(
    await fetchUIJSON("/auth/login", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body),
    })
  );
}

export async function logout(): Promise<void> {
  try {
    await fetchUIJSON("/auth/logout", { method: "POST" });
  } finally {
    clearCSRFToken();
  }
}
