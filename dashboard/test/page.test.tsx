import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/context/TenantContext", () => ({ useTenant: () => ({ tenant: "ai8" }) }));
vi.mock("@/components/ActivityPanel", () => ({ default: ({ loading, error, activity }: { loading: boolean; error: string | null; activity: unknown[] }) => <div>{loading ? "loading" : error || `activity:${activity.length}`}</div> }));
vi.mock("@/components/ConversationPanel", () => ({ default: () => <div>conversation</div> }));

import Home from "@/app/page";

describe("dashboard home", () => {
  it("loads the activity endpoint and renders returned activity", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ json: async () => ({ activity: [{ id: "one" }] }) }));
    render(<Home />);
    await waitFor(() => expect(screen.getByText("activity:1")).toBeVisible());
    vi.unstubAllGlobals();
  });
});
