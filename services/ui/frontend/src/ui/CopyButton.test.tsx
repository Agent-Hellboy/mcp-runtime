import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { CopyButton } from "./CopyButton";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("CopyButton", () => {
  it("copies the whole value and confirms success", async () => {
    const user = userEvent.setup({ writeToClipboard: false });
    const writeText = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();

    render(<CopyButton value="http://example.test/very/long/endpoint" label="Copy endpoint" testId="copy" />);
    await user.click(screen.getByTestId("copy"));

    expect(writeText).toHaveBeenCalledWith("http://example.test/very/long/endpoint");
    expect(await screen.findByText("Copied")).toBeInTheDocument();
  });

  it("reports a clipboard failure instead of pretending it worked", async () => {
    const user = userEvent.setup({ writeToClipboard: false });
    vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(new Error("denied"));

    render(<CopyButton value="secret-endpoint" label="Copy endpoint" testId="copy" />);
    await user.click(screen.getByTestId("copy"));

    // The value stays on screen next to the control, so a denied clipboard is
    // recoverable by selecting it manually.
    expect(await screen.findByText(/Copy failed/)).toBeInTheDocument();
  });
});
