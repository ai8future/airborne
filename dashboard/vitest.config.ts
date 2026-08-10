import { defineConfig } from "vitest/config";
import path from "node:path";

export default defineConfig({
  resolve: { alias: { "@": path.resolve(__dirname, "src") } },
  test: {
    environment: "jsdom",
    exclude: ["e2e/**", "node_modules/**"],
    setupFiles: ["./test/setup.ts"],
    coverage: {
      provider: "v8",
      reporter: ["text", "json-summary", "html"],
      // Instrument every executable dashboard source file. CSS, SVG assets,
      // framework declarations, and build/config files are outside `src/**/*`
      // or do not match the TypeScript extension, so no production behavior is
      // hidden behind a hand-maintained coverage allowlist.
      include: ["src/**/*.{ts,tsx}"],
      thresholds: { statements: 70, lines: 70, branches: 60, functions: 70 },
    },
  },
});
