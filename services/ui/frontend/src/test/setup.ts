import "@testing-library/jest-dom/vitest";
import * as axeMatchers from "vitest-axe/matchers";
import { beforeEach, expect } from "vitest";

expect.extend(axeMatchers);

// Hash routing is real navigation state: without this, a test that navigated
// to #/keys would leave the next test starting there.
beforeEach(() => {
  window.location.hash = "";
});
