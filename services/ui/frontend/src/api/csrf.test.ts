import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  CSRFError,
  apiURL,
  clearCSRFToken,
  fetchJSON,
  hasCSRFToken,
  isUnsafeMethod,
  setCSRFToken,
} from "./client";
import { login, logout, readAuthStatus } from "./auth";

beforeEach(() => {
  delete window.MCP_API_BASE;
  clearCSRFToken();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  clearCSRFToken();
});

function okFetch(body: unknown = {}) {
  const fetchMock = vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    json: async () => body,
    text: async () => JSON.stringify(body),
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

describe("isUnsafeMethod", () => {
  it("treats only the read methods as safe", () => {
    for (const method of ["GET", "head", "OPTIONS", "trace"]) {
      expect(isUnsafeMethod(method)).toBe(false);
    }
    for (const method of ["POST", "put", "PATCH", "delete", "PURGE"]) {
      expect(isUnsafeMethod(method)).toBe(true);
    }
  });
});

describe("apiURL write routing", () => {
  it("routes allowlisted writes through the session proxy", () => {
    expect(apiURL("/user/api-keys", "/api/v1", "POST")).toBe("/api/ui/v1/user/api-keys");
    expect(apiURL("/user/api-keys/uk_1", "/api/v1", "DELETE")).toBe(
      "/api/ui/v1/user/api-keys/uk_1"
    );
  });

  it("leaves non-allowlisted writes on the public API base", () => {
    // Wrong method for the path.
    expect(apiURL("/user/api-keys", "/api/v1", "DELETE")).toBe("/api/v1/user/api-keys");
    expect(apiURL("/user/api-keys/uk_1", "/api/v1", "POST")).toBe("/api/v1/user/api-keys/uk_1");
    // Not a write route at all.
    expect(apiURL("/runtime/servers", "/api/v1", "POST")).toBe("/api/v1/runtime/servers");
    // Nested beyond one segment.
    expect(apiURL("/user/api-keys/uk_1/extra", "/api/v1", "DELETE")).toBe(
      "/api/v1/user/api-keys/uk_1/extra"
    );
  });
});

describe("CSRF header attachment", () => {
  it("sends the token on unsafe methods only", async () => {
    setCSRFToken("token-abc");
    const fetchMock = okFetch({ keys: [] });

    await fetchJSON("/user/api-keys");
    expect(new Headers(fetchMock.mock.calls[0][1].headers).get("x-csrf-token")).toBeNull();

    await fetchJSON("/user/api-keys", { method: "POST", body: "{}" });
    expect(new Headers(fetchMock.mock.calls[1][1].headers).get("x-csrf-token")).toBe("token-abc");
  });

  it("ignores a caller-supplied CSRF token", async () => {
    setCSRFToken("real-token");
    const fetchMock = okFetch();

    await fetchJSON("/user/api-keys", {
      method: "POST",
      headers: { "X-CSRF-Token": "attacker-chosen" },
      body: "{}",
    });

    expect(new Headers(fetchMock.mock.calls[0][1].headers).get("x-csrf-token")).toBe("real-token");
  });

  it("sends no token when the session has none", async () => {
    const fetchMock = okFetch();

    await fetchJSON("/user/api-keys", { method: "POST", body: "{}" });

    expect(new Headers(fetchMock.mock.calls[0][1].headers).get("x-csrf-token")).toBeNull();
  });

  it("maps a csrf_failed 403 to CSRFError", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: false,
        status: 403,
        text: async () => `{"error":"csrf_failed"}`,
      })
    );

    await expect(fetchJSON("/user/api-keys", { method: "POST" })).rejects.toBeInstanceOf(CSRFError);
  });

  it("leaves other 403s as plain errors", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: false,
        status: 403,
        text: async () => `{"error":"forbidden"}`,
      })
    );

    const err = await fetchJSON("/user/api-keys", { method: "POST" }).catch((e) => e);
    expect(err).toBeInstanceOf(Error);
    expect(err).not.toBeInstanceOf(CSRFError);
  });
});

describe("auth token lifecycle", () => {
  it("captures the token from login and never returns it to callers", async () => {
    okFetch({ authenticated: true, principal: { role: "user" }, csrf_token: "from-login" });

    const status = await login({ email: "a@b.c", password: "pw" });

    expect(hasCSRFToken()).toBe(true);
    expect(JSON.stringify(status)).not.toContain("from-login");
  });

  it("recovers the token from status after a reload", async () => {
    okFetch({ authenticated: true, principal: { role: "user" }, csrf_token: "from-status" });

    await readAuthStatus();

    expect(hasCSRFToken()).toBe(true);
  });

  it("clears the token when status reports signed out", async () => {
    setCSRFToken("stale");
    okFetch({ authenticated: false });

    await readAuthStatus();

    expect(hasCSRFToken()).toBe(false);
  });

  it("clears the token on logout even if the request fails", async () => {
    setCSRFToken("live");
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: false, status: 500, text: async () => "boom" })
    );

    await logout().catch(() => {});

    expect(hasCSRFToken()).toBe(false);
  });

  it("never persists the token to browser storage", async () => {
    okFetch({ authenticated: true, principal: { role: "user" }, csrf_token: "secret-token" });

    await login({ email: "a@b.c", password: "pw" });

    expect(window.localStorage.length).toBe(0);
    expect(window.sessionStorage.length).toBe(0);
    expect(document.cookie).not.toContain("secret-token");
  });
});
