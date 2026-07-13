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
      include: [
        "src/app/api/**/*.ts",
        "src/lib/**/*.ts",
        "src/components/ActivityPanel.tsx",
        "src/components/TestPanel.tsx",
        "src/components/TenantSelector.tsx",
      ],
      thresholds: { statements: 70, lines: 70, branches: 60, functions: 70 },
    },
  },
});
