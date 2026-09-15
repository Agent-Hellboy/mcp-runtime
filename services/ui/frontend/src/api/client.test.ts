import { afterEach, describe, expect, it, vi } from "vitest";

import { UnauthorizedError, apiURL, fetchJSON, fetchUIJSON, withQuery } from "./client";
import { readRuntimeConfig } from "./config";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  delete window.MCP_API_BASE;
});

describe("apiURL", () => {
  it("routes allowlisted catalog GETs through the UI session proxy", () => {
    expect(apiURL("/runtime/namespaces", "/api/v1")).toBe("/api/ui/v1/runtime/namespaces");
    expect(apiURL("/runtime/servers?namespace=mcp-servers", "/api/v1")).toBe(
      "/api/ui/v1/runtime/servers?namespace=mcp-servers"
    );
    expect(apiURL("/runtime/tools", "/api/v1")).toBe("/api/ui/v1/runtime/tools");
  });

  it("keeps unmigrated paths on the public API base", () => {
    expect(apiURL("/runtime/grants", "/api/v1")).toBe("/api/v1/runtime/grants");
    expect(apiURL("/runtime/servers/mcp-servers/demo", "/api/v1")).toBe(
      "/api/v1/runtime/servers/mcp-servers/demo"
    );
  });

  it("does not route mutations through the GET-only session proxy", () => {
    expect(apiURL("/runtime/servers", "/api/v1", "POST")).toBe("/api/v1/runtime/servers");
  });
});

describe("readRuntimeConfig", () => {
  it("defaults apiBase to /api/v1", () => {
    expect(readRuntimeConfig().apiBase).toBe("/api/v1");
  });
});

describe("fetchJSON", () => {
  it("uses same-origin credentials and never sets an authorization header", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ servers: [] }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await expect(fetchJSON("/runtime/servers?namespace=mcp-servers")).resolves.toEqual({
      servers: [],
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/ui/v1/runtime/servers?namespace=mcp-servers");
    expect(init.credentials).toBe("same-origin");
    const headers = new Headers(init.headers);
    expect(headers.get("authorization")).toBeNull();
    expect(headers.get("x-api-key")).toBeNull();
  });

  it("strips caller-supplied credentials before fetch", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ ok: true }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await fetchJSON("/runtime/namespaces", {
      headers: {
        Authorization: "Bearer leaked",
        "x-api-key": "leaked-key",
      },
    });

    const headers = new Headers((fetchMock.mock.calls[0] as [string, RequestInit])[1].headers);
    expect(headers.get("authorization")).toBeNull();
    expect(headers.get("x-api-key")).toBeNull();
  });

  it("throws UnauthorizedError on 401", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: false,
        status: 401,
        text: async () => `{"error":"unauthorized"}`,
      })
    );

    await expect(fetchJSON("/runtime/tools")).rejects.toBeInstanceOf(UnauthorizedError);
  });
});

describe("withQuery", () => {
  it("appends only non-empty parameters", () => {
    expect(withQuery("/runtime/servers", { namespace: "mcp-servers" })).toBe(
      "/runtime/servers?namespace=mcp-servers"
    );
    expect(withQuery("/runtime/servers", { namespace: "  " })).toBe("/runtime/servers");
    expect(withQuery("/runtime/servers", { namespace: undefined })).toBe("/runtime/servers");
  });
});

describe("fetchUIJSON", () => {
  it("calls UI-origin session paths without the API base", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ authenticated: false }),
    });
    vi.stubGlobal("fetch", fetchMock);
    window.MCP_API_BASE = "/api/v1";

    await expect(fetchUIJSON("/auth/status")).resolves.toEqual({ authenticated: false });
    expect(fetchMock.mock.calls[0][0]).toBe("/auth/status");
    expect((fetchMock.mock.calls[0][1] as RequestInit).credentials).toBe("same-origin");
  });

  it("strips caller-supplied credential headers", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ authenticated: true }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await fetchUIJSON("/auth/login", {
      method: "POST",
      headers: { Authorization: "Bearer leaked", "x-api-key": "leaked-key" },
      body: JSON.stringify({ email: "a@b.c", password: "x" }),
    });

    const headers = new Headers((fetchMock.mock.calls[0] as [string, RequestInit])[1].headers);
    expect(headers.get("authorization")).toBeNull();
    expect(headers.get("x-api-key")).toBeNull();
  });

  it("refuses paths outside the UI session allowlist", async () => {
    await expect(fetchUIJSON("/auth/admin-check")).rejects.toThrow(
      "unsupported UI origin path: /auth/admin-check"
    );
  });

  it("maps 401 to UnauthorizedError", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: false, status: 401, text: async () => "unauthorized" })
    );

    await expect(fetchUIJSON("/auth/status")).rejects.toBeInstanceOf(UnauthorizedError);
  });
});
