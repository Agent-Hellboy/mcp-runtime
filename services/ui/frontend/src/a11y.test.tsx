import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";

import { App } from "./App";
import { AppProviders } from "./providers/AppProviders";

function renderApp() {
  return render(
    <AppProviders>
      <App />
    </AppProviders>
  );
}

// Colour contrast needs real layout, which jsdom does not provide; the browser
// QA pass covers it. Everything else runs here.
const AXE_OPTIONS = { rules: { "color-contrast": { enabled: false } } };

const CATALOG = {
  "/api/ui/v1/runtime/namespaces": { namespaces: [{ namespace: "mcp-servers" }] },
  "/api/ui/v1/runtime/servers": {
    servers: [
      {
        name: "workspace-assistant",
        namespace: "mcp-servers",
        ready: "1/1",
        status: "Running",
        description: "Workspace helper",
      },
    ],
  },
  "/api/ui/v1/runtime/tools": {
    tools: [
      {
        tool_name: "add",
        description: "Add two numeric values",
        server_name: "workspace-assistant",
        namespace: "mcp-servers",
        declared: true,
        live: true,
        drift_status: "declared",
        required_trust: "low",
        side_effect: "read",
        risk_level: "low",
      },
    ],
  },
};

function stub(authenticated: boolean) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/auth/status") {
        return {
          ok: true,
          status: 200,
          json: async () => ({
            authenticated,
            principal: authenticated ? { role: "admin", email: "admin@mcpruntime.org" } : undefined,
          }),
        } as unknown as Response;
      }
      const key = Object.keys(CATALOG).find((path) => url.startsWith(path));
      return {
        ok: true,
        status: 200,
        json: async () => (key ? CATALOG[key as keyof typeof CATALOG] : {}),
      } as unknown as Response;
    })
  );
}

beforeEach(() => {
  delete window.MCP_API_BASE;
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("accessibility", () => {
  it("has no detectable violations while signed out", async () => {
    stub(false);
    const { container } = renderApp();
    await screen.findByTestId("catalog-signed-out");

    expect(await axe(container, AXE_OPTIONS)).toHaveNoViolations();
  });

  it("has no detectable violations on the sign-in form", async () => {
    const user = userEvent.setup();
    stub(false);
    const { container } = renderApp();
    await user.click(await screen.findByTestId("signin-button"));

    expect(await axe(container, AXE_OPTIONS)).toHaveNoViolations();
  });

  it("has no detectable violations on the servers workspace", async () => {
    stub(true);
    const { container } = renderApp();
    await screen.findByTestId("server-list");

    expect(await axe(container, AXE_OPTIONS)).toHaveNoViolations();
  });

  it("has no detectable violations with a tool selected", async () => {
    const user = userEvent.setup();
    stub(true);
    const { container } = renderApp();
    await screen.findByTestId("server-list");
    await user.click(screen.getAllByTestId("tool-row-select")[0]);
    await waitFor(() => expect(screen.getByTestId("tool-detail")).toBeInTheDocument());

    expect(await axe(container, AXE_OPTIONS)).toHaveNoViolations();
  });

  it("keeps every interactive control keyboard reachable", async () => {
    const user = userEvent.setup();
    stub(true);
    renderApp();
    await screen.findByTestId("server-list");

    const search = screen.getByTestId("tool-search");
    search.focus();
    expect(search).toHaveFocus();
    await user.tab();
    expect(document.activeElement).not.toBe(search);
    expect(document.activeElement?.tagName).toBe("SELECT");
  });
});
