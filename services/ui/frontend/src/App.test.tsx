import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { App } from "./App";

describe("App", () => {
  it("renders the legacy dashboard bridge", () => {
    render(<App />);

    const frame = screen.getByTitle("MCP Sentinel dashboard");
    expect(frame).toHaveAttribute("src", "/legacy/index.html");
  });
});
