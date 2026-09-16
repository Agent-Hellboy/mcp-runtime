import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { App } from "../../App";
import { AppProviders } from "../../providers/AppProviders";

// Exercises the admin workspace through the real shell, so the navigation and
// the route guard are covered together rather than in isolation.

const CATALOG: Record<string, unknown> = {
  "/runtime/namespaces": { namespaces: [] },
  "/runtime/servers": { servers: [] },
  "/runtime/tools": { tools: [] },
  "/runtime/grants": { grants: [] },
  "/runtime/sessions": { sessions: [] },
  "/runtime/teams": { teams: [] },
  "/runtime/components": { components: [] },
  "/admin/operations": { users: [], audit_logs: [], images: [] },
};

function stub(role: string | undefined, authenticated = true) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url === "/auth/status") {
      return {
        ok: true,
        status: 200,
        json: async () => ({
          authenticated,
          principal: authenticated ? { role, email: "someone@mcpruntime.org" } : undefined,
        }),
      } as unknown as Response;
    }
    const key = Object.keys(CATALOG).find((path) => url.includes(path));
    return {
      ok: true,
      status: 200,
      json: async () => (key ? CATALOG[key] : {}),
    } as unknown as Response;
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function renderApp() {
  return render(
    <AppProviders>
      <App />
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

describe("admin workspace navigation", () => {
  it("offers the Administration tab to an admin and opens it", async () => {
    const user = userEvent.setup();
    stub("admin");

    renderApp();
    const tab = await screen.findByTestId("workspace-tab-admin");

    await user.click(tab);
    expect(await screen.findByTestId("admin-section-teams")).toBeInTheDocument();
  });

  it("hides the Administration tab from a tenant user", async () => {
    stub("user");

    renderApp();
    await screen.findByTestId("workspace-tab-servers");

    expect(screen.queryByTestId("workspace-tab-admin")).not.toBeInTheDocument();
    expect(screen.getByTestId("workspace-tab-legacy")).toBeInTheDocument();
  });

  it("hides the Administration tab from a signed-out visitor", async () => {
    stub(undefined, false);

    renderApp();
    await screen.findByTestId("workspace-tab-servers");

    expect(screen.queryByTestId("workspace-tab-admin")).not.toBeInTheDocument();
  });

  it("drops out of the admin workspace when the session signs out", async () => {
    const user = userEvent.setup();
    const fetchMock = stub("admin");

    renderApp();
    await user.click(await screen.findByTestId("workspace-tab-admin"));
    await screen.findByTestId("admin-section-teams");

    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ authenticated: false }),
    } as unknown as Response);
    await user.click(screen.getByTestId("logout-button"));

    await waitFor(() =>
      expect(screen.queryByTestId("admin-section-teams")).not.toBeInTheDocument()
    );
    expect(screen.queryByTestId("workspace-tab-admin")).not.toBeInTheDocument();
  });

  it("keeps the legacy fallback reachable for unmigrated admin actions", async () => {
    const user = userEvent.setup();
    stub("admin");

    renderApp();
    await user.click(await screen.findByTestId("workspace-tab-legacy"));

    expect(screen.getByTitle("MCP Sentinel dashboard")).toHaveAttribute(
      "src",
      "/legacy/index.html"
    );
  });
});

// Access control (grants/sessions) is its own top-level workspace, reachable
// by any authenticated user - admin or tenant - matching the legacy
// governance tab's data-auth-required gate and the backend's plain auth()
// (not adminOnly()) middleware on /runtime/grants and /runtime/sessions.
describe("access control workspace navigation", () => {
  it("offers the Access Control tab to a tenant user and opens it", async () => {
    const user = userEvent.setup();
    stub("user");

    renderApp();
    const tab = await screen.findByTestId("workspace-tab-access");

    await user.click(tab);
    expect(await screen.findByTestId("grants-table")).toBeInTheDocument();
  });

  it("offers the Access Control tab to an admin too", async () => {
    const user = userEvent.setup();
    stub("admin");

    renderApp();
    const tab = await screen.findByTestId("workspace-tab-access");

    await user.click(tab);
    expect(await screen.findByTestId("grants-table")).toBeInTheDocument();
  });

  it("hides the Access Control tab from a signed-out visitor", async () => {
    stub(undefined, false);

    renderApp();
    await screen.findByTestId("workspace-tab-servers");

    expect(screen.queryByTestId("workspace-tab-access")).not.toBeInTheDocument();
  });

  it("drops out of the access workspace when the session signs out", async () => {
    const user = userEvent.setup();
    const fetchMock = stub("user");

    renderApp();
    await user.click(await screen.findByTestId("workspace-tab-access"));
    await screen.findByTestId("grants-table");

    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ authenticated: false }),
    } as unknown as Response);
    await user.click(screen.getByTestId("logout-button"));

    await waitFor(() => expect(screen.queryByTestId("grants-table")).not.toBeInTheDocument());
    expect(screen.queryByTestId("workspace-tab-access")).not.toBeInTheDocument();
  });
});
