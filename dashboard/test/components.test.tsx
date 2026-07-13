import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import ActivityPanel from "@/components/ActivityPanel";
import TestPanel from "@/components/TestPanel";

const entry = {
  id: "activity-1", thread_id: "thread-1", tenant: "ai8", user_id: "user", content: "A request", provider: "gemini", model: "gemini-2.0-20250101",
  input_tokens: 1250, output_tokens: 300, tokens_used: 1550, cost_usd: 0.02, thread_cost_usd: 0.03, processing_time_ms: 1500, status: "success", timestamp: "2026-01-01T10:00:00Z",
};

describe("dashboard components", () => {
  it("renders activity loading, error, empty, and selection controls", async () => {
    const onPauseToggle = vi.fn(), onClear = vi.fn(), onSelectThread = vi.fn();
    const { rerender } = render(<ActivityPanel activity={[]} loading error={null} paused={false} onPauseToggle={onPauseToggle} onClear={onClear} onSelectThread={onSelectThread} selectedThreadId={null} />);
    expect(screen.getByText("Loading activity...")).toBeVisible();
    rerender(<ActivityPanel activity={[]} loading={false} error="backend unavailable" paused={false} onPauseToggle={onPauseToggle} onClear={onClear} onSelectThread={onSelectThread} selectedThreadId={null} />);
    expect(screen.getByText("backend unavailable")).toBeVisible();
    rerender(<ActivityPanel activity={[]} loading={false} error={null} paused={true} onPauseToggle={onPauseToggle} onClear={onClear} onSelectThread={onSelectThread} selectedThreadId={null} />);
    expect(screen.getByText(/No recent activity/)).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Resume" })); await userEvent.click(screen.getByRole("button", { name: "Clear" }));
    expect(onPauseToggle).toHaveBeenCalledOnce(); expect(onClear).toHaveBeenCalledOnce();
    rerender(<ActivityPanel activity={[entry]} loading={false} error={null} paused={false} onPauseToggle={onPauseToggle} onClear={onClear} onSelectThread={onSelectThread} selectedThreadId={null} />);
    await userEvent.click(screen.getByText("A request"));
    expect(onSelectThread).toHaveBeenCalledWith("thread-1");
    expect(screen.getByText("1.3K")).toBeVisible();
  });

  it("submits a test request and displays the response", async () => {
    const fetch = vi.fn().mockResolvedValue({ json: async () => ({ reply: "confirmed", provider: "openai", model: "gpt-test", input_tokens: 2, output_tokens: 3, processing_ms: 4 }) });
    vi.stubGlobal("fetch", fetch);
    render(<TestPanel />);
    await userEvent.click(screen.getByRole("button", { name: "OpenAI" }));
    await userEvent.click(screen.getByRole("button", { name: "Send Test Message" }));
    expect(await screen.findByText("confirmed")).toBeVisible();
    expect(fetch).toHaveBeenCalledWith("/api/test", expect.objectContaining({ method: "POST" }));
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toMatchObject({ provider: "openai" });
    vi.unstubAllGlobals();
  });

  it("displays request failures and disables blank submissions", async () => {
    const fetch = vi.fn().mockRejectedValue(new Error("network down")); vi.stubGlobal("fetch", fetch);
    render(<TestPanel />);
    const textarea = screen.getByRole("textbox", { name: "Test Prompt" });
    fireEvent.change(textarea, { target: { value: "" } });
    expect(screen.getByRole("button", { name: "Send Test Message" })).toBeDisabled();
    fireEvent.change(textarea, { target: { value: "test" } });
    await userEvent.click(screen.getByRole("button", { name: "Send Test Message" }));
    expect(await screen.findByText("network down")).toBeVisible();
    vi.unstubAllGlobals();
  });
});
