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
    expect(screen.getByTestId("workspace-tab-servers")).toBeInTheDocument();
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
    await user.click(await screen.findByTestId("account-trigger"));
    await user.click(screen.getByTestId("logout-button"));

    await waitFor(() =>
      expect(screen.queryByTestId("admin-section-teams")).not.toBeInTheDocument()
    );
    expect(screen.queryByTestId("workspace-tab-admin")).not.toBeInTheDocument();
  });

  it("refuses a deep link into administration for a tenant user", async () => {
    window.location.hash = "#/admin/access";
    stub("user");

    renderApp();
    await screen.findByTestId("workspace-tab-servers");

    // The guard and the route both fail closed, so nothing admin renders.
    expect(screen.queryByTestId("admin-section-teams")).not.toBeInTheDocument();
    expect(screen.queryByTestId("grants-table")).not.toBeInTheDocument();
  });

  it("puts the administration section in the URL", async () => {
    const user = userEvent.setup();
    stub("admin");

    renderApp();
    await user.click(await screen.findByTestId("workspace-tab-admin"));
    await user.click(await screen.findByTestId("admin-section-teams"));

    expect(window.location.hash).toBe("#/admin/teams");
  });
});

// Access control is a top-level workspace reachable by any authenticated
// principal - admin or tenant - matching the backend's plain auth()
// middleware on /runtime/grants and /runtime/sessions.
describe("access control workspace navigation", () => {
  it("offers Access control to a tenant user and opens it", async () => {
    const user = userEvent.setup();
    stub("user");

    renderApp();
    await user.click(await screen.findByTestId("workspace-tab-access"));

    expect(await screen.findByTestId("grants-table")).toBeInTheDocument();
    expect(window.location.hash).toBe("#/access");
  });

  it("offers Access control to an admin too", async () => {
    const user = userEvent.setup();
    stub("admin");

    renderApp();
    await user.click(await screen.findByTestId("workspace-tab-access"));

    expect(await screen.findByTestId("grants-table")).toBeInTheDocument();
  });

  it("hides Access control from a signed-out visitor", async () => {
    stub(undefined, false);

    renderApp();
    await screen.findByTestId("workspace-tab-servers");

    expect(screen.queryByTestId("workspace-tab-access")).not.toBeInTheDocument();
  });
});
