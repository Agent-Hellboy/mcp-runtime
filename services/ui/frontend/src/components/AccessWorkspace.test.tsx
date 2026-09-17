import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";

import { AccessWorkspace } from "./AccessWorkspace";
import { AppProviders } from "../providers/AppProviders";
import type { AuthStatus } from "../api/types";

// Colour contrast needs real layout, which jsdom does not provide; the
// browser QA pass covers it.
const AXE_OPTIONS = { rules: { "color-contrast": { enabled: false } } };

const ADMIN: AuthStatus = {
  authenticated: true,
  principal: { role: "admin", email: "admin@mcpruntime.org" },
};
const TENANT: AuthStatus = {
  authenticated: true,
  principal: { role: "user", email: "test@mcpruntime.org" },
};
const SIGNED_OUT: AuthStatus = { authenticated: false };

const GRANTS = {
  grants: [
    {
      name: "acme-readonly",
      namespace: "mcp-servers",
      serverRef: { name: "workspace-assistant" },
      subject: { humanID: "alice@example.com", teamID: "acme" },
      maxTrust: "low",
      allowedSideEffects: ["read"],
      disabled: false,
    },
    {
      name: "acme-retired",
      namespace: "mcp-servers",
      serverRef: { name: "workspace-assistant" },
      subject: { humanID: "bob@example.com" },
      maxTrust: "high",
      allowedSideEffects: ["read", "write"],
      disabled: true,
    },
  ],
};

const SESSIONS = {
  sessions: [
    {
      name: "sess-1",
      namespace: "mcp-servers",
      serverRef: { name: "workspace-assistant" },
      subject: { humanID: "alice@example.com", agentID: "agent-7" },
      consentedTrust: "low",
      revoked: false,
      expiresAt: "2026-10-01T00:00:00Z",
    },
  ],
};

const EVENTS = {
  events: [
    {
      timestamp: new Date().toISOString(),
      namespace: "mcp-servers",
      tool_name: "add",
      decision: "allow",
      payload: { matched_grant: "acme-readonly", matched_grant_namespace: "mcp-servers" },
    },
  ],
};

function stubAccessApi(overrides: Record<string, { status?: number; body?: unknown }> = {}) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    const route = Object.keys(overrides).find((key) => url.includes(key));
    if (route) {
      const { status = 200, body = {} } = overrides[route];
      return {
        ok: status < 400,
        status,
        json: async () => body,
        text: async () => JSON.stringify(body),
      } as unknown as Response;
    }
    const body = url.includes("/runtime/grants")
      ? GRANTS
      : url.includes("/runtime/sessions")
        ? SESSIONS
        : url.includes("/events")
          ? EVENTS
          : {};
    return { ok: true, status: 200, json: async () => body } as unknown as Response;
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function renderAccess(auth: AuthStatus) {
  return render(
    <AppProviders>
      <AccessWorkspace auth={auth} onSignIn={() => {}} />
    </AppProviders>
  );
}

beforeEach(() => {
  delete window.MCP_API_BASE;
  delete window.MCP_DEFAULTS;
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("AccessWorkspace role gating", () => {
  it("prompts sign-in for a signed-out visitor and issues no request", () => {
    const fetchMock = stubAccessApi();

    renderAccess(SIGNED_OUT);

    expect(screen.getByTestId("access-signed-out")).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  // The backend registers /runtime/grants and /runtime/sessions with its
  // plain auth() middleware, not adminOnly() (services/runtime-api/routes.go),
  // data-admin-only. A non-admin, authenticated tenant user must see and
  // manage their own grants/sessions exactly like an admin does.
  it("gives a tenant (non-admin) user the same full read access an admin gets", async () => {
    stubAccessApi();

    renderAccess(TENANT);

    const table = await screen.findByTestId("grants-table");
    expect(within(table).getByText("alice@example.com / acme")).toBeInTheDocument();
    expect(screen.getByTestId("sessions-table")).toBeInTheDocument();
    expect(screen.getByTestId("access-stats")).toHaveTextContent("1");
  });

  // This verifies the frontend wiring (CSRF header, PATCH method) reaches the
  // proxy for a tenant session - it does not assert the backend authorizes
  // every mutation. The runtime API separately blocks non-admin writes when
  // scoped to the shared catalog namespace ("shared catalog namespace is
  // read-only for access resources", scopedAccessWriteNamespaceForPrincipal);
  // a tenant's own team namespace is where these mutations are expected to
  // succeed in practice.
  it("lets a tenant user mutate grants and sessions through the CSRF-backed proxy", async () => {
    const fetchMock = stubAccessApi();
    vi.spyOn(window, "confirm").mockReturnValue(true);

    renderAccess(TENANT);
    await screen.findByTestId("grants-table");

    await userEvent.click(screen.getAllByTestId("grant-toggle")[0]);
    await userEvent.click(screen.getAllByTestId("session-toggle")[0]);

    await waitFor(() => {
      const writes = fetchMock.mock.calls.filter((call) => (call[1] as RequestInit)?.method);
      expect(writes.map((call) => (call[1] as RequestInit).method)).toEqual(
        expect.arrayContaining(["PATCH", "PATCH"])
      );
    });
  });

  it("also gives an admin full access", async () => {
    stubAccessApi();

    renderAccess(ADMIN);

    expect(await screen.findByTestId("grants-table")).toBeInTheDocument();
  });

  // The backend 403s a non-admin request with no namespace and no team
  // namespace (scopedNamespaceForPrincipal); default to the first namespace
  // /runtime/namespaces returns for that principal. Admin keeps the
  // cluster-wide default (no namespace param) unchanged.
  it("defaults a tenant session to their first visible namespace so the first read succeeds", async () => {
    const fetchMock = stubAccessApi({
      "/runtime/namespaces": { body: { namespaces: [{ namespace: "mcp-servers" }] } },
    });

    renderAccess(TENANT);

    expect(screen.getByTestId("access-loading")).toBeInTheDocument();

    await waitFor(() => {
      const urls = fetchMock.mock.calls.map((call) => String(call[0]));
      expect(urls.some((url) => url.includes("/runtime/grants?namespace=mcp-servers"))).toBe(
        true
      );
      expect(urls.some((url) => url.includes("/runtime/sessions?namespace=mcp-servers"))).toBe(
        true
      );
    });

    // The grants/sessions request must never have gone out with an empty
    // namespace first - that would 403 for a non-admin and flash an error
    // before the corrected, scoped request replaces it.
    const urls = fetchMock.mock.calls.map((call) => String(call[0]));
    expect(urls).not.toContain("/api/ui/v1/runtime/grants");
    expect(urls).not.toContain("/api/ui/v1/runtime/sessions");
  });

  it("leaves an admin session scoped to every namespace by default", async () => {
    const fetchMock = stubAccessApi({
      "/runtime/namespaces": {
        body: { namespaces: [{ namespace: "" }, { namespace: "mcp-servers" }] },
      },
    });

    renderAccess(ADMIN);
    await screen.findByTestId("grants-table");

    const urls = fetchMock.mock.calls.map((call) => String(call[0]));
    expect(urls.some((url) => url === "/api/ui/v1/runtime/grants")).toBe(true);
    expect(urls.some((url) => url.includes("namespace="))).toBe(false);
  });
});

describe("AccessWorkspace data", () => {
  it("reads grants and sessions through the session-backed proxy without leaking credentials", async () => {
    const fetchMock = stubAccessApi();

    renderAccess(TENANT);
    await screen.findByTestId("grants-table");

    const urls = fetchMock.mock.calls.map((call) => String(call[0]));
    expect(urls).toEqual(
      expect.arrayContaining(["/api/ui/v1/runtime/grants", "/api/ui/v1/runtime/sessions"])
    );
    for (const call of fetchMock.mock.calls) {
      const init = call[1] as RequestInit;
      expect(init.credentials).toBe("same-origin");
      const headers = new Headers(init.headers);
      expect(headers.get("authorization")).toBeNull();
      expect(headers.get("x-api-key")).toBeNull();
    }
  });

  it("filters grants and sessions by term", async () => {
    const user = userEvent.setup();
    stubAccessApi();

    renderAccess(TENANT);
    await screen.findByTestId("grants-table");

    await user.type(screen.getByTestId("access-filter"), "retired");
    expect(screen.getAllByTestId("grants-table-row")).toHaveLength(1);

    await user.clear(screen.getByTestId("access-filter"));
    await user.type(screen.getByTestId("access-filter"), "zzz-no-match");
    expect(screen.getByTestId("grants-table-empty")).toHaveTextContent(
      "No grants match this filter."
    );
  });

  it("shows an empty state when no grants exist", async () => {
    stubAccessApi({
      "/runtime/grants": { body: { grants: [] } },
      "/runtime/sessions": { body: { sessions: [] } },
    });

    renderAccess(TENANT);

    expect(await screen.findByTestId("grants-table-empty")).toHaveTextContent(
      "No access grants found."
    );
  });

  it("shows an error state when the grant read fails", async () => {
    stubAccessApi({ "/runtime/grants": { status: 500, body: { error: "boom" } } });

    renderAccess(TENANT);

    const error = await screen.findByTestId("grants-error", {}, { timeout: 5000 });
    expect(error).toHaveTextContent("Access grants could not be loaded.");
    expect(error).toHaveAttribute("role", "alert");
  });

  it("shows a session-expired state on 401", async () => {
    stubAccessApi({ "/runtime/grants": { status: 401, body: { error: "unauthorized" } } });

    renderAccess(TENANT);

    expect(await screen.findByTestId("grants-unauthorized")).toHaveTextContent(
      "Your session expired."
    );
  });
});

describe("AccessWorkspace drill-down", () => {
  it("opens a grant detail with its context and returns via the back path", async () => {
    const user = userEvent.setup();
    stubAccessApi();

    renderAccess(TENANT);
    await screen.findByTestId("grants-table");

    await user.click(screen.getAllByTestId("grant-drilldown")[0]);

    const detail = await screen.findByTestId("access-detail");
    expect(detail).toHaveTextContent("acme-readonly");
    expect(screen.getByTestId("access-detail-kicker")).toHaveTextContent(
      "mcp-servers / MCPAccessGrant"
    );
    expect(detail).toHaveTextContent("alice@example.com / acme");

    await user.click(screen.getByTestId("access-detail-back"));
    await waitFor(() => expect(screen.getByTestId("grants-table")).toBeInTheDocument());
    expect(screen.queryByTestId("access-detail")).not.toBeInTheDocument();
  });

  it("opens a session detail labelled as an agent session", async () => {
    const user = userEvent.setup();
    stubAccessApi();

    renderAccess(TENANT);
    await screen.findByTestId("sessions-table");

    await user.click(screen.getAllByTestId("session-drilldown")[0]);

    expect(await screen.findByTestId("access-detail-kicker")).toHaveTextContent(
      "mcp-servers / MCPAgentSession"
    );
    expect(screen.getByTestId("access-detail")).toHaveTextContent("sess-1");
  });

  it("surfaces the analytics outage as an activity error, not a blank table", async () => {
    const user = userEvent.setup();
    stubAccessApi({ "/events": { status: 502, body: { error: "upstream_error" } } });

    renderAccess(TENANT);
    await screen.findByTestId("grants-table");
    await user.click(screen.getAllByTestId("grant-drilldown")[0]);

    const error = await screen.findByTestId("access-activity-error", {}, { timeout: 5000 });
    expect(error).toHaveTextContent("Activity is unavailable.");
    expect(error).toHaveTextContent("analytics service");
  });

  it("tells a non-admin the activity view needs admin access, not that analytics is down", async () => {
    const user = userEvent.setup();
    stubAccessApi({
      "/events": { status: 403, body: { error: "forbidden", message: "insufficient permissions" } },
    });

    renderAccess(TENANT);
    await screen.findByTestId("grants-table");
    await user.click(screen.getAllByTestId("grant-drilldown")[0]);

    const error = await screen.findByTestId("access-activity-forbidden", {}, { timeout: 5000 });
    expect(error).toHaveTextContent("Admin access required.");
    expect(screen.queryByTestId("access-activity-error")).not.toBeInTheDocument();
  });
});

describe("AccessWorkspace accessibility", () => {
  it("has no detectable violations on the signed-out prompt", async () => {
    stubAccessApi();
    const { container } = renderAccess(SIGNED_OUT);

    expect(await axe(container, AXE_OPTIONS)).toHaveNoViolations();
  });

  it("has no detectable violations for a tenant user", async () => {
    stubAccessApi();
    const { container } = renderAccess(TENANT);
    await screen.findByTestId("grants-table");

    expect(await axe(container, AXE_OPTIONS)).toHaveNoViolations();
  });

  it("has no detectable violations on the grant drill-down", async () => {
    const user = userEvent.setup();
    stubAccessApi();
    const { container } = renderAccess(TENANT);
    await screen.findByTestId("grants-table");
    await user.click(screen.getAllByTestId("grant-drilldown")[0]);
    await screen.findByTestId("access-detail");

    expect(await axe(container, AXE_OPTIONS)).toHaveNoViolations();
  });
});
