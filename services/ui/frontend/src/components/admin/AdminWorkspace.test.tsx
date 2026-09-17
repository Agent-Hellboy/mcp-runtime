import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AdminWorkspace } from "./AdminWorkspace";
import { AppProviders } from "../../providers/AppProviders";
import type { AuthStatus } from "../../api/types";

const ADMIN: AuthStatus = {
  authenticated: true,
  principal: { role: "admin", email: "admin@mcpruntime.org" },
};
const TENANT: AuthStatus = {
  authenticated: true,
  principal: { role: "user", email: "test@mcpruntime.org" },
};
const SIGNED_OUT: AuthStatus = { authenticated: false };

const TEAMS = {
  teams: [{ id: "t-1", slug: "verify", name: "Verify Team", namespace: "mcp-team-verify" }],
};

const COMPONENTS = {
  components: [
    {
      key: "operator",
      display: "Operator",
      namespace: "mcp-runtime",
      kind: "Deployment",
      resource: "mcp-runtime-operator",
      status: "Ready",
      ready: "2/2",
    },
    {
      key: "clickhouse",
      display: "ClickHouse",
      namespace: "mcp-sentinel",
      kind: "StatefulSet",
      resource: "clickhouse",
      status: "NotReady",
      ready: "0/1",
      message: "CrashLoopBackOff",
    },
  ],
};

const OPERATIONS = {
  users: [
    {
      id: "u-1",
      email: "admin@mcpruntime.org",
      role: "admin",
      login_count: 25,
      failed_action_count: 0,
      registry_credentials: 1,
      api_keys: 2,
    },
  ],
  audit_logs: [
    {
      action: "server_publish",
      resource: "workspace-assistant",
      namespace: "mcp-servers",
      status: "success",
      created_at: "2026-09-15T10:00:00Z",
    },
  ],
  images: [
    {
      image_ref: "registry/demo:1",
      action: "publish",
      status: "success",
      created_at: "2026-09-15T10:00:00Z",
    },
  ],
};

function stubAdminApi(overrides: Record<string, { status?: number; body?: unknown }> = {}) {
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
    const body = url.includes("/runtime/teams")
      ? TEAMS
      : url.includes("/runtime/components")
        ? COMPONENTS
        : url.includes("/admin/operations")
          ? OPERATIONS
          : {};
    return { ok: true, status: 200, json: async () => body } as unknown as Response;
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function renderAdmin(auth: AuthStatus) {
  return render(
    <AppProviders>
      <AdminWorkspace auth={auth} onSignIn={() => {}} />
    </AppProviders>
  );
}

beforeEach(() => {
  delete window.MCP_API_BASE;
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("AdminWorkspace role gating", () => {
  it("refuses a signed-out visitor and issues no admin request", () => {
    const fetchMock = stubAdminApi();

    renderAdmin(SIGNED_OUT);

    expect(screen.getByTestId("admin-signed-out")).toBeInTheDocument();
    expect(screen.queryByTestId("admin-section-teams")).not.toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("refuses a non-admin session and issues no admin request", () => {
    const fetchMock = stubAdminApi();

    renderAdmin(TENANT);

    const refusal = screen.getByTestId("admin-forbidden");
    expect(refusal).toHaveTextContent("This workspace is restricted to administrators.");
    expect(refusal).toHaveAttribute("role", "alert");
    // No admin control of any kind is rendered for a tenant user. Access
    // control (grants/sessions) is deliberately not admin-gated at all - it
    // lives in the separate AccessWorkspace - so it is not asserted here.
    expect(screen.queryByTestId("admin-section-teams")).not.toBeInTheDocument();
    expect(screen.queryByTestId("teams-table")).not.toBeInTheDocument();
    expect(screen.queryByTestId("grafana-link")).not.toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("treats an unknown role as non-admin", () => {
    stubAdminApi();

    renderAdmin({ authenticated: true, principal: {} });

    expect(screen.getByTestId("admin-forbidden")).toBeInTheDocument();
  });

  it("renders the admin-only sections for an admin principal", async () => {
    stubAdminApi();

    renderAdmin(ADMIN);

    expect(await screen.findByTestId("teams-table")).toBeInTheDocument();
    for (const section of ["teams", "operations", "platform", "analytics"]) {
      expect(screen.getByTestId(`admin-section-${section}`)).toBeInTheDocument();
    }
    // Access control moved out to its own workspace; it is not an
    // Administration section any more.
    expect(screen.queryByTestId("admin-section-access")).not.toBeInTheDocument();
  });
});

describe("AdminWorkspace sections", () => {
  it("renders teams by default", async () => {
    stubAdminApi();

    renderAdmin(ADMIN);

    const table = await screen.findByTestId("teams-table");
    expect(within(table).getByText("Verify Team")).toBeInTheDocument();
    expect(within(table).getByText("mcp-team-verify")).toBeInTheDocument();
  });

  it("renders operations users, audit trail, and image activity", async () => {
    const user = userEvent.setup();
    stubAdminApi();

    renderAdmin(ADMIN);
    await screen.findByTestId("teams-table");
    await user.click(screen.getByTestId("admin-section-operations"));

    expect(await screen.findByTestId("operations-users-table")).toHaveTextContent(
      "admin@mcpruntime.org"
    );
    expect(screen.getByTestId("operations-audit-table")).toHaveTextContent("server_publish");
    expect(screen.getByTestId("operations-images-table")).toHaveTextContent("registry/demo:1");
  });

  it("refetches operations scoped to the applied user filter", async () => {
    const user = userEvent.setup();
    const fetchMock = stubAdminApi();

    renderAdmin(ADMIN);
    await screen.findByTestId("teams-table");
    await user.click(screen.getByTestId("admin-section-operations"));
    await screen.findByTestId("operations-users-table");

    await user.type(screen.getByTestId("operations-user-filter"), "alice@example.com");
    await user.click(screen.getByTestId("operations-apply"));

    await waitFor(() => {
      const urls = fetchMock.mock.calls.map((call) => String(call[0]));
      expect(urls.some((url) => url.includes("user=alice%40example.com"))).toBe(true);
    });
  });

  it("renders platform health and keeps Grafana a plain forward-auth link", async () => {
    const user = userEvent.setup();
    stubAdminApi();

    renderAdmin(ADMIN);
    await screen.findByTestId("teams-table");
    await user.click(screen.getByTestId("admin-section-platform"));

    const table = await screen.findByTestId("platform-table");
    expect(within(table).getByText("Operator")).toBeInTheDocument();
    expect(within(table).getByText("CrashLoopBackOff")).toBeInTheDocument();
    expect(screen.getByTestId("platform-stats")).toHaveTextContent("1");

    // Must stay a direct href so the platform ingress forward-auth still applies.
    expect(screen.getByTestId("grafana-link")).toHaveAttribute("href", "/grafana");
    expect(screen.getByTestId("prometheus-link")).toHaveAttribute("href", "/prometheus");
  });
});
