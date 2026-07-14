import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/context/TenantContext", () => ({
  TenantProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/TenantSelector", () => ({
  default: () => <div>tenant-selector</div>,
}));

import RootLayout, { metadata } from "@/app/layout";

describe("root dashboard layout", () => {
  it("renders the application shell and declares dashboard metadata", () => {
    const markup = renderToStaticMarkup(
      <RootLayout>
        <section>dashboard-content</section>
      </RootLayout>,
    );

    expect(metadata).toMatchObject({
      title: "Airborne",
      description: "Live activity monitoring for Airborne LLM Gateway",
    });
    expect(markup).toContain('<html lang="en"');
    expect(markup).toContain("Airborne");
    expect(markup).toContain("tenant-selector");
    expect(markup).toContain("dashboard-content");
  });
});
