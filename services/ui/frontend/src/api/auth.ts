import { UnauthorizedError, fetchUIJSON } from "./client";
import type { AuthStatus } from "./types";

function asAuthStatus(value: unknown): AuthStatus {
  if (!value || typeof value !== "object") {
    return { authenticated: false };
  }
  const record = value as Record<string, unknown>;
  return {
    authenticated: record.authenticated === true,
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

export type LoginInput = { email: string; password: string } | { apiKey: string };

export async function login(input: LoginInput): Promise<AuthStatus> {
  const body = "apiKey" in input
    ? { api_key: input.apiKey }
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
  await fetchUIJSON("/auth/logout", { method: "POST" });
}
