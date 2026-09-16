import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { App } from "./App";
import { AppProviders } from "./providers/AppProviders";

function renderApp() {
  return render(
    <AppProviders>
      <App />
    </AppProviders>
  );
}

type Route = { status: number; body: unknown };

function stubRoutes(routes: Record<string, Route | Route[]>) {
  const calls: Array<{ url: string; init: RequestInit }> = [];
  const pending = new Map<string, Route[]>();
  for (const [key, value] of Object.entries(routes)) {
    pending.set(key, Array.isArray(value) ? [...value] : [value]);
  }

  const fetchMock = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const url = String(input);
    calls.push({ url, init });
    const match = [...pending.keys()].find((key) => url.startsWith(key));
    const queue = match ? pending.get(match) : undefined;
    const route = queue && (queue.length > 1 ? queue.shift() : queue[0]);
    if (!route) {
      return { ok: false, status: 404, text: async () => "not stubbed" } as unknown as Response;
    }
    return {
      ok: route.status < 400,
      status: route.status,
      json: async () => route.body,
      text: async () => JSON.stringify(route.body),
    } as unknown as Response;
  });

  vi.stubGlobal("fetch", fetchMock);
  return { fetchMock, calls };
}

const SIGNED_OUT: Route = { status: 200, body: { authenticated: false } };
const ADMIN: Route = {
  status: 200,
  body: { authenticated: true, principal: { role: "admin", email: "admin@mcpruntime.org" } },
};
const EMPTY_CATALOG = {
  "/api/ui/v1/runtime/namespaces": { status: 200, body: { namespaces: [] } },
  "/api/ui/v1/runtime/servers": { status: 200, body: { servers: [] } },
  "/api/ui/v1/runtime/tools": { status: 200, body: { tools: [] } },
};

beforeEach(() => {
  delete window.MCP_API_BASE;
  window.localStorage.removeItem("mcp-sentinel-theme");
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  window.localStorage.removeItem("mcp-sentinel-theme");
});

describe("App", () => {
  it("renders the signed-out shell with a sign-in action", async () => {
    stubRoutes({ "/auth/status": SIGNED_OUT });

    renderApp();

    expect(await screen.findByTestId("catalog-signed-out")).toBeInTheDocument();
    expect(screen.getByTestId("account-state")).toHaveTextContent("Signed out");
    expect(screen.getByTestId("signin-button")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Control Plane Dashboard" })).toBeInTheDocument();
  });

  it("switches themes and persists the preference", async () => {
    const user = userEvent.setup();
    stubRoutes({ "/auth/status": SIGNED_OUT });

    renderApp();

    const toggle = await screen.findByTestId("theme-toggle");
    expect(toggle).toHaveTextContent("Dark");
    expect(document.documentElement.dataset.theme).toBe("dark");

    await user.click(toggle);

    expect(toggle).toHaveTextContent("Light");
    expect(toggle).toHaveAttribute("aria-label", "Switch to dark mode");
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(window.localStorage.getItem("mcp-sentinel-theme")).toBe("light");
  });

  it("signs in through the UI session endpoint and loads the catalog", async () => {
    const user = userEvent.setup();
    const { fetchMock, calls } = stubRoutes({
      "/auth/status": SIGNED_OUT,
      "/auth/login": ADMIN,
      ...EMPTY_CATALOG,
    });

    renderApp();
    await user.click(await screen.findByTestId("signin-button"));

    await user.type(screen.getByTestId("login-email"), "admin@mcpruntime.org");
    await user.type(screen.getByTestId("login-password"), "admin@123");
    await user.click(screen.getByTestId("login-submit"));

    await waitFor(() =>
      expect(screen.getByTestId("account-state")).toHaveTextContent("admin@mcpruntime.org")
    );
    expect(screen.getByTestId("logout-button")).toBeInTheDocument();

    const loginCall = calls.find((call) => call.url === "/auth/login");
    expect(loginCall?.init.credentials).toBe("same-origin");
    expect(new Headers(loginCall?.init.headers).get("authorization")).toBeNull();
    expect(String(loginCall?.init.body)).not.toContain("api_key");

    await waitFor(() =>
      expect(fetchMock.mock.calls.map((call) => String(call[0]))).toContain(
        "/api/ui/v1/runtime/servers"
      )
    );
  });

  it("signs in with an API key without exposing it after submit", async () => {
    const user = userEvent.setup();
    const { calls } = stubRoutes({
      "/auth/status": SIGNED_OUT,
      "/auth/login": ADMIN,
      ...EMPTY_CATALOG,
    });

    renderApp();
    await user.click(await screen.findByTestId("signin-button"));
    await user.type(screen.getByTestId("login-api-key"), "ui-key");
    await user.click(screen.getByTestId("login-submit"));

    await waitFor(() => expect(screen.getByTestId("logout-button")).toBeInTheDocument());
    const loginCall = calls.find((call) => call.url === "/auth/login");
    expect(String(loginCall?.init.body)).toBe(JSON.stringify({ api_key: "ui-key" }));
    expect(document.body.innerHTML).not.toContain("ui-key");
  });

  it("signs in with a Google credential", async () => {
    const user = userEvent.setup();
    window.MCP_GOOGLE_CLIENT_ID = "test-client-id";
    const initialize = vi.fn();
    window.google = { accounts: { id: { initialize, renderButton: vi.fn() } } };
    const { calls } = stubRoutes({
      "/auth/status": SIGNED_OUT,
      "/auth/login": ADMIN,
      ...EMPTY_CATALOG,
    });

    renderApp();
    await user.click(await screen.findByTestId("signin-button"));
    await waitFor(() => expect(initialize).toHaveBeenCalledTimes(1));

    const { callback } = initialize.mock.calls[0][0];
    callback({ credential: "google-id-token" });

    await waitFor(() => expect(screen.getByTestId("logout-button")).toBeInTheDocument());
    const loginCall = calls.find((call) => call.url === "/auth/login");
    expect(String(loginCall?.init.body)).toBe(JSON.stringify({ id_token: "google-id-token" }));

    delete window.google;
    delete window.MCP_GOOGLE_CLIENT_ID;
  });

  it("shows a sign-in error without leaving the form", async () => {
    const user = userEvent.setup();
    stubRoutes({
      "/auth/status": SIGNED_OUT,
      "/auth/login": { status: 401, body: { error: "unauthorized" } },
    });

    renderApp();
    await user.click(await screen.findByTestId("signin-button"));
    await user.type(screen.getByTestId("login-email"), "nobody@example.com");
    await user.type(screen.getByTestId("login-password"), "wrong");
    await user.click(screen.getByTestId("login-submit"));

    expect(await screen.findByTestId("login-error")).toHaveTextContent(
      "That email, password, or API key was not accepted."
    );
    expect(screen.getByTestId("login-form")).toBeInTheDocument();
  });

  it("returns to the signed-out state after sign out", async () => {
    const user = userEvent.setup();
    const { calls } = stubRoutes({
      "/auth/status": ADMIN,
      "/auth/logout": { status: 200, body: { authenticated: false } },
      ...EMPTY_CATALOG,
    });

    renderApp();
    await user.click(await screen.findByTestId("logout-button"));

    await waitFor(() =>
      expect(screen.getByTestId("account-state")).toHaveTextContent("Signed out")
    );
    expect(screen.getByTestId("signin-button")).toBeInTheDocument();
    expect(calls.some((call) => call.url === "/auth/logout" && call.init.method === "POST")).toBe(
      true
    );
  });

  it("never offers a legacy fallback tab or iframe", async () => {
    stubRoutes({ "/auth/status": ADMIN, ...EMPTY_CATALOG });

    renderApp();
    await screen.findByTestId("server-list-empty");

    expect(screen.queryByTestId("workspace-tab-legacy")).not.toBeInTheDocument();
    expect(screen.queryByTitle("MCP Sentinel dashboard")).not.toBeInTheDocument();
  });
});
