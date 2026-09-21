import { render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { GoogleSignInButton } from "./GoogleSignInButton";

function stubGoogleIdentityServices() {
  const initialize = vi.fn();
  const renderButton = vi.fn();
  window.google = { accounts: { id: { initialize, renderButton } } };
  return { initialize, renderButton };
}

beforeEach(() => {
  delete window.MCP_GOOGLE_CLIENT_ID;
  delete window.google;
  document.querySelectorAll('script[src*="accounts.google.com"]').forEach((node) => node.remove());
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("GoogleSignInButton", () => {
  it("renders nothing when no client ID is configured for this deployment", () => {
    const { container } = render(<GoogleSignInButton onCredential={() => {}} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("initializes and renders the Google button once the client ID is configured", async () => {
    window.MCP_GOOGLE_CLIENT_ID = "test-client-id";
    const { initialize, renderButton } = stubGoogleIdentityServices();

    render(<GoogleSignInButton onCredential={() => {}} />);

    await waitFor(() => expect(initialize).toHaveBeenCalledTimes(1));
    expect(initialize).toHaveBeenCalledWith(
      expect.objectContaining({ client_id: "test-client-id" })
    );
    expect(renderButton).toHaveBeenCalledTimes(1);
  });

  it("forwards the credential to onCredential", async () => {
    window.MCP_GOOGLE_CLIENT_ID = "test-client-id";
    const { initialize } = stubGoogleIdentityServices();
    const onCredential = vi.fn();

    render(<GoogleSignInButton onCredential={onCredential} />);
    await waitFor(() => expect(initialize).toHaveBeenCalledTimes(1));

    const { callback } = initialize.mock.calls[0][0];
    callback({ credential: "id-token-value" });

    expect(onCredential).toHaveBeenCalledWith("id-token-value");
  });

  it("ignores a callback response with no credential", async () => {
    window.MCP_GOOGLE_CLIENT_ID = "test-client-id";
    const { initialize } = stubGoogleIdentityServices();
    const onCredential = vi.fn();
    const onError = vi.fn();

    render(<GoogleSignInButton onCredential={onCredential} onError={onError} />);
    await waitFor(() => expect(initialize).toHaveBeenCalledTimes(1));

    const { callback } = initialize.mock.calls[0][0];
    callback({});

    expect(onCredential).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledWith("Google sign-in did not return a credential. Try again.");
  });

  it("loads the Google Identity Services script only once across multiple mounts", () => {
    window.MCP_GOOGLE_CLIENT_ID = "test-client-id";
    // window.google stays unset here (unlike the other cases) so the
    // component takes its real script-injection path instead of the
    // already-loaded short-circuit - script.onload never fires in jsdom,
    // which is fine: the dedup happens synchronously, before that point.

    render(<GoogleSignInButton onCredential={() => {}} />);
    render(<GoogleSignInButton onCredential={() => {}} />);

    const scripts = document.querySelectorAll(
      'script[src="https://accounts.google.com/gsi/client"]'
    );
    expect(scripts).toHaveLength(1);
  });
});
