import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";

// jsdom does not implement this browser layout API. Components may use it for
// progressive enhancement, so provide the no-op browser contract in tests.
Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
  configurable: true,
  value: vi.fn(),
});

afterEach(() => cleanup());
