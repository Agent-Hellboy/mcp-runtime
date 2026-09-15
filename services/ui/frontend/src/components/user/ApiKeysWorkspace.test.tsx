import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiKeysWorkspace } from "./ApiKeysWorkspace";
import { AppProviders } from "../../providers/AppProviders";
import { setCSRFToken } from "../../api/client";
import type { AuthStatus } from "../../api/types";

const TENANT: AuthStatus = {
  authenticated: true,
  principal: { role: "user", subject: "user-1", email: "dev@example.com" },
};

const NO_IDENTITY: AuthStatus = {
  authenticated: true,
  principal: { role: "admin", auth_type: "ui_api_key" },
};

const KEYS = {
  keys: [
    {
      id: "uk_1",
      name: "laptop",
      prefix: "mcpu_abc",
      created_at: "2026-09-01T10:00:00Z",
      revoked: false,
    },
    {
      id: "uk_2",
      name: "old-ci",
      prefix: "mcpu_xyz",
      created_at: "2026-08-01T10:00:00Z",
      revoked: true,
      revoked_at: "2026-08-20T10:00:00Z",
    },
  ],
};

type Handler = (url: string, init: RequestInit) => { status: number; body: unknown };

function stub(handler: Handler) {
  const calls: Array<{ url: string; init: RequestInit }> = [];
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const url = String(input);
    calls.push({ url, init });
    const { status, body } = handler(url, init);
    return {
      ok: status < 400,
      status,
      json: async () => body,
      text: async () => JSON.stringify(body),
    } as unknown as Response;
  });
  vi.stubGlobal("fetch", fetchMock);
  return { fetchMock, calls };
}

function listOnly(): Handler {
  return () => ({ status: 200, body: KEYS });
}

function renderKeys(auth: AuthStatus = TENANT) {
  return render(
    <AppProviders>
      <ApiKeysWorkspace auth={auth} onSignIn={() => {}} />
    </AppProviders>
  );
}

beforeEach(() => {
  delete window.MCP_API_BASE;
  setCSRFToken("csrf-token-1");
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  setCSRFToken("");
});

describe("ApiKeysWorkspace", () => {
  it("prompts signed-out users to sign in and fetches nothing", () => {
    const { fetchMock } = stub(listOnly());

    render(
      <AppProviders>
        <ApiKeysWorkspace auth={{ authenticated: false }} onSignIn={() => {}} />
      </AppProviders>
    );

    expect(screen.getByTestId("keys-signed-out")).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("hides personal keys from a session with no user identity", () => {
    const { fetchMock } = stub(listOnly());

    renderKeys(NO_IDENTITY);

    expect(screen.getByTestId("keys-no-identity")).toBeInTheDocument();
    expect(screen.queryByTestId("create-key-form")).not.toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("shows a loading state and then the key table", async () => {
    stub(listOnly());

    renderKeys();
    expect(screen.getByTestId("keys-loading")).toBeInTheDocument();

    await screen.findByTestId("api-keys-table");
    expect(screen.getAllByTestId("api-key-row")).toHaveLength(2);
    expect(screen.getByTestId("api-keys-table")).toHaveTextContent("laptop");
    expect(screen.getByTestId("api-keys-table")).toHaveTextContent("Revoked");
  });

  it("reads keys through the session proxy without credential headers", async () => {
    const { calls } = stub(listOnly());

    renderKeys();
    await screen.findByTestId("api-keys-table");

    expect(calls[0].url).toBe("/api/ui/v1/user/api-keys");
    expect(calls[0].init.credentials).toBe("same-origin");
    const headers = new Headers(calls[0].init.headers);
    expect(headers.get("authorization")).toBeNull();
    expect(headers.get("x-api-key")).toBeNull();
    // Reads must not carry the CSRF token.
    expect(headers.get("x-csrf-token")).toBeNull();
  });

  it("renders an empty state when the user has no keys", async () => {
    stub(() => ({ status: 200, body: { keys: [] } }));

    renderKeys();

    expect(await screen.findByTestId("api-keys-empty")).toBeInTheDocument();
  });

  it("renders an error state and retries", async () => {
    const user = userEvent.setup();
    let failing = true;
    stub(() =>
      failing ? { status: 500, body: { error: "boom" } } : { status: 200, body: KEYS }
    );

    renderKeys();

    const error = await screen.findByTestId("keys-error", {}, { timeout: 5000 });
    expect(error).toHaveAttribute("role", "alert");

    failing = false;
    await user.click(within(error).getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.getByTestId("api-keys-table")).toBeInTheDocument());
  });

  it("renders a session-expired state on 401", async () => {
    stub(() => ({ status: 401, body: { error: "unauthorized" } }));

    renderKeys();

    expect(await screen.findByTestId("keys-unauthorized")).toHaveTextContent(
      "Your session expired."
    );
  });

  it("validates the key name before sending a request", async () => {
    const user = userEvent.setup();
    const { calls } = stub(listOnly());

    renderKeys();
    await screen.findByTestId("api-keys-table");
    const before = calls.length;

    await user.click(screen.getByTestId("create-key-submit"));

    expect(screen.getByTestId("create-key-error")).toHaveTextContent(
      "Enter a name so you can recognise this key later."
    );
    expect(calls).toHaveLength(before);
  });

  it("creates a key with the CSRF header and shows it exactly once", async () => {
    const user = userEvent.setup();
    const { calls } = stub((url, init) => {
      if (init.method === "POST") {
        return {
          status: 200,
          body: { key: { id: "uk_3", name: "ci", prefix: "mcpu_new", revoked: false }, api_key: "mcpu_supersecret" },
        };
      }
      return { status: 200, body: KEYS };
    });

    renderKeys();
    await screen.findByTestId("api-keys-table");

    await user.type(screen.getByTestId("create-key-name"), "ci");
    await user.click(screen.getByTestId("create-key-submit"));

    const notice = await screen.findByTestId("one-time-key");
    expect(within(notice).getByTestId("one-time-key-value")).toHaveTextContent("mcpu_supersecret");

    const post = calls.find((call) => call.init.method === "POST");
    expect(post?.url).toBe("/api/ui/v1/user/api-keys");
    const headers = new Headers(post?.init.headers);
    expect(headers.get("x-csrf-token")).toBe("csrf-token-1");
    expect(headers.get("authorization")).toBeNull();

    // Dismissing destroys the only copy.
    await user.click(screen.getByTestId("one-time-key-dismiss"));
    expect(screen.queryByTestId("one-time-key")).not.toBeInTheDocument();
    expect(document.body.innerHTML).not.toContain("mcpu_supersecret");
  });

  it("does not retain one-time key material across a remount", async () => {
    const user = userEvent.setup();
    stub((url, init) =>
      init.method === "POST"
        ? { status: 200, body: { key: { id: "uk_3", name: "ci" }, api_key: "mcpu_supersecret" } }
        : { status: 200, body: KEYS }
    );

    const view = renderKeys();
    await screen.findByTestId("api-keys-table");
    await user.type(screen.getByTestId("create-key-name"), "ci");
    await user.click(screen.getByTestId("create-key-submit"));
    await screen.findByTestId("one-time-key");

    // A refresh or navigation loses the value permanently, by design.
    view.unmount();
    renderKeys();
    await screen.findByTestId("api-keys-table");

    expect(screen.queryByTestId("one-time-key")).not.toBeInTheDocument();
    expect(document.body.innerHTML).not.toContain("mcpu_supersecret");
    expect(window.localStorage.length).toBe(0);
    expect(window.sessionStorage.length).toBe(0);
  });

  it("disables the submit button while a create is in flight", async () => {
    const user = userEvent.setup();
    let release: (() => void) | undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
        if (init.method === "POST") {
          await gate;
          return {
            ok: true,
            status: 200,
            json: async () => ({ key: { id: "uk_3" }, api_key: "k" }),
            text: async () => "",
          } as unknown as Response;
        }
        return {
          ok: true,
          status: 200,
          json: async () => KEYS,
          text: async () => "",
        } as unknown as Response;
      })
    );

    renderKeys();
    await screen.findByTestId("api-keys-table");
    await user.type(screen.getByTestId("create-key-name"), "ci");
    await user.click(screen.getByTestId("create-key-submit"));

    await waitFor(() =>
      expect(screen.getByTestId("create-key-submit")).toBeDisabled()
    );
    expect(screen.getByTestId("create-key-submit")).toHaveTextContent("Creating…");

    release?.();
    await waitFor(() => expect(screen.getByTestId("create-key-submit")).toBeEnabled());
  });

  it("surfaces a CSRF failure as a reload prompt", async () => {
    const user = userEvent.setup();
    stub((url, init) =>
      init.method === "POST"
        ? { status: 403, body: { error: "csrf_failed" } }
        : { status: 200, body: KEYS }
    );

    renderKeys();
    await screen.findByTestId("api-keys-table");
    await user.type(screen.getByTestId("create-key-name"), "ci");
    await user.click(screen.getByTestId("create-key-submit"));

    expect(await screen.findByTestId("create-key-error")).toHaveTextContent(
      "Your session security token expired. Reload the page and try again."
    );
  });

  it("surfaces a forbidden create failure", async () => {
    const user = userEvent.setup();
    stub((url, init) =>
      init.method === "POST"
        ? { status: 403, body: { error: "forbidden" } }
        : { status: 200, body: KEYS }
    );

    renderKeys();
    await screen.findByTestId("api-keys-table");
    await user.type(screen.getByTestId("create-key-name"), "ci");
    await user.click(screen.getByTestId("create-key-submit"));

    expect(await screen.findByTestId("create-key-error")).toHaveTextContent(
      "You do not have permission to do that."
    );
  });

  it("requires an explicit confirmation before revoking", async () => {
    const user = userEvent.setup();
    const { calls } = stub((url, init) =>
      init.method === "DELETE" ? { status: 200, body: {} } : { status: 200, body: KEYS }
    );

    renderKeys();
    await screen.findByTestId("api-keys-table");

    await user.click(screen.getAllByTestId("revoke-key")[0]);
    const confirm = screen.getByTestId("revoke-confirm");
    // Confirmation names the affected key.
    expect(confirm).toHaveTextContent("Revoke laptop?");
    expect(calls.some((call) => call.init.method === "DELETE")).toBe(false);

    await user.click(screen.getByTestId("revoke-confirm-cancel"));
    expect(screen.queryByTestId("revoke-confirm")).not.toBeInTheDocument();
    expect(calls.some((call) => call.init.method === "DELETE")).toBe(false);

    await user.click(screen.getAllByTestId("revoke-key")[0]);
    await user.click(screen.getByTestId("revoke-confirm-yes"));

    await waitFor(() => {
      const del = calls.find((call) => call.init.method === "DELETE");
      expect(del?.url).toBe("/api/ui/v1/user/api-keys/uk_1");
      expect(new Headers(del?.init.headers).get("x-csrf-token")).toBe("csrf-token-1");
    });
  });

  it("surfaces a revoke failure without losing the table", async () => {
    const user = userEvent.setup();
    stub((url, init) =>
      init.method === "DELETE"
        ? { status: 500, body: { error: "upstream_error" } }
        : { status: 200, body: KEYS }
    );

    renderKeys();
    await screen.findByTestId("api-keys-table");
    await user.click(screen.getAllByTestId("revoke-key")[0]);
    await user.click(screen.getByTestId("revoke-confirm-yes"));

    expect(await screen.findByTestId("revoke-error")).toBeInTheDocument();
    expect(screen.getByTestId("api-keys-table")).toBeInTheDocument();
  });

  it("revoked keys expose no revoke action", async () => {
    stub(listOnly());

    renderKeys();
    await screen.findByTestId("api-keys-table");

    // Two keys, one already revoked, so only one revoke button.
    expect(screen.getAllByTestId("revoke-key")).toHaveLength(1);
  });
});
