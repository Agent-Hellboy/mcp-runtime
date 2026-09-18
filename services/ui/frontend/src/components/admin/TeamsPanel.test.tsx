import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { TeamsPanel } from "./TeamsPanel";
import { AppProviders } from "../../providers/AppProviders";

const TEAMS = {
  teams: [
    { id: "t-1", slug: "acme", name: "Acme", namespace: "mcp-team-acme" },
    { id: "t-2", slug: "globex", name: "Globex", namespace: "mcp-team-globex" },
  ],
};

const MEMBERS: Record<string, unknown> = {
  acme: { members: [{ user_id: "u-1", email: "alice@example.com", role: "owner" }] },
  globex: { members: [{ user_id: "u-2", email: "bob@example.com", role: "member" }] },
};

// Resolves the members read only when the test releases it, so the panel can be
// observed mid-switch.
function stubTeams(options: { gateSlug?: string } = {}) {
  let release: (() => void) | undefined;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });

  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    const memberMatch = url.match(/\/runtime\/teams\/([^/]+)\/members/);
    if (memberMatch) {
      const slug = decodeURIComponent(memberMatch[1]);
      if (options.gateSlug === slug) {
        await gate;
      }
      return {
        ok: true,
        status: 200,
        json: async () => MEMBERS[slug] ?? { members: [] },
      } as unknown as Response;
    }
    return { ok: true, status: 200, json: async () => TEAMS } as unknown as Response;
  });

  vi.stubGlobal("fetch", fetchMock);
  return { fetchMock, release: () => release?.() };
}

function renderTeams() {
  return render(
    <AppProviders>
      <TeamsPanel onSignIn={() => {}} />
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

describe("TeamsPanel", () => {
  it("selects the first team and shows only its members", async () => {
    stubTeams();

    renderTeams();

    await screen.findByTestId("teams-table");
    const members = await screen.findByTestId("team-members-table");
    expect(within(members).getByText("alice@example.com")).toBeInTheDocument();
    expect(within(members).queryByText("bob@example.com")).not.toBeInTheDocument();
  });

  it("never shows the previous team's members under a newly selected team", async () => {
    const user = userEvent.setup();
    const { release } = stubTeams({ gateSlug: "globex" });

    renderTeams();
    await screen.findByTestId("team-members-table");

    await user.selectOptions(screen.getByTestId("team-select-input"), "globex");

    // While the new read is in flight the heading has changed, so the old rows
    // must be gone rather than mislabelled.
    expect(await screen.findByTestId("team-members-loading")).toBeInTheDocument();
    expect(screen.queryByText("alice@example.com")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Members of Globex" })).toBeInTheDocument();

    release();
    await waitFor(() =>
      expect(within(screen.getByTestId("team-members-table")).getByText("bob@example.com")).toBeInTheDocument()
    );
  });

  it("confirms member removal by name and consequence before writing", async () => {
    const user = userEvent.setup();
    const { fetchMock } = stubTeams();

    renderTeams();
    await screen.findByTestId("team-members-table");

    await user.click(screen.getByTestId("member-remove"));
    const dialog = await screen.findByTestId("teams-confirm");
    expect(dialog).toHaveTextContent("Remove alice@example.com from Acme?");
    expect(dialog).toHaveTextContent("The account itself is not deleted.");
    expect(fetchMock.mock.calls.filter((call) => (call[1] as RequestInit)?.method === "DELETE")).toHaveLength(0);

    await user.click(screen.getByTestId("teams-confirm-cancel"));
    expect(screen.queryByTestId("teams-confirm")).not.toBeInTheDocument();
    expect(fetchMock.mock.calls.filter((call) => (call[1] as RequestInit)?.method === "DELETE")).toHaveLength(0);
  });

  it("describes account creation as creating an account, not inviting one", async () => {
    const user = userEvent.setup();
    stubTeams();

    renderTeams();
    await screen.findByTestId("team-members-table");

    await user.click(screen.getByTestId("team-user-toggle"));
    const form = await screen.findByTestId("team-user-form");
    expect(form).toHaveTextContent("It does not invite an existing user");
  });
});
