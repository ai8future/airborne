import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/context/TenantContext", () => ({ useTenant: () => ({ tenant: "ai8" }) }));
vi.mock("@/components/ActivityPanel", () => ({
  default: ({
    loading,
    error,
    activity,
    paused,
    onPauseToggle,
    onClear,
    onSelectThread,
  }: {
    loading: boolean;
    error: string | null;
    activity: unknown[];
    paused: boolean;
    onPauseToggle: () => void;
    onClear: () => void;
    onSelectThread: (threadId: string) => void;
  }) => (
    <div>
      <span>{loading ? "loading" : error || `activity:${activity.length}`}</span>
      <span>{paused ? "paused" : "running"}</span>
      <button onClick={onPauseToggle}>toggle polling</button>
      <button onClick={onClear}>clear activity</button>
      <button onClick={() => onSelectThread("selected-thread")}>select thread</button>
    </div>
  ),
}));
vi.mock("@/components/ConversationPanel", () => ({
  default: ({ selectedThreadId }: { selectedThreadId: string | null }) => (
    <div>conversation:{selectedThreadId || "none"}</div>
  ),
}));

import Home from "@/app/page";

describe("dashboard home", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("loads, selects, clears, pauses, and resumes the activity workflow", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ json: async () => ({ activity: [{ id: "one" }] }) });
    vi.stubGlobal("fetch", fetchMock);
    let poll: () => void = () => {};
    const nativeSetInterval = window.setInterval.bind(window);
    vi.stubGlobal("setInterval", vi.fn((handler: TimerHandler, delay?: number) => {
      if (delay === 3000) {
        poll = handler as () => void;
        return 1;
      }
      return nativeSetInterval(handler, delay);
    }));

    render(<Home />);
    await waitFor(() => expect(screen.getByText("activity:1")).toBeVisible());
    expect(fetchMock).toHaveBeenCalledWith("/api/activity?limit=50&tenant_id=ai8");

    fireEvent.click(screen.getByRole("button", { name: "select thread" }));
    expect(screen.getByText("conversation:selected-thread")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "clear activity" }));
    expect(screen.getByText("activity:0")).toBeVisible();

    await act(async () => poll());
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));

    fireEvent.click(screen.getByRole("button", { name: "toggle polling" }));
    await waitFor(() => expect(screen.getByText("paused")).toBeVisible());
    const pausedFetchCount = fetchMock.mock.calls.length;
    await act(async () => poll());
    expect(fetchMock).toHaveBeenCalledTimes(pausedFetchCount);

    fireEvent.click(screen.getByRole("button", { name: "toggle polling" }));
    await waitFor(() => expect(screen.getByText("running")).toBeVisible());
    await act(async () => poll());
    expect(fetchMock.mock.calls.length).toBeGreaterThan(pausedFetchCount);
  });

  it("surfaces an API-declared error", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ json: async () => ({ activity: [], error: "backend unavailable" }) }),
    );
    render(<Home />);
    expect(await screen.findByText("backend unavailable")).toBeVisible();
  });

  it("surfaces a failed fetch and clears the loading state", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("connection reset")));
    render(<Home />);
    expect(await screen.findByText("Failed to fetch activity: connection reset")).toBeVisible();
    expect(screen.queryByText("loading")).not.toBeInTheDocument();
  });
});
