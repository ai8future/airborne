import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const replace = vi.fn();
vi.mock("next/navigation", () => ({
  useSearchParams: () => new URLSearchParams("tenant=ai8"),
  useRouter: () => ({ replace }),
  usePathname: () => "/",
}));

import TenantSelector from "@/components/TenantSelector";
import { TenantProvider, useTenant } from "@/context/TenantContext";

function TenantProbe() {
  const { tenant, setTenant } = useTenant();
  return <button onClick={() => setTenant("zztest")}>{tenant}</button>;
}

function InvalidProbe() {
  useTenant();
  return null;
}

describe("tenant dashboard surface", () => {
  afterEach(() => replace.mockReset());

  it("updates the tenant context and preserves it in the URL", async () => {
    render(
      <TenantProvider>
        <TenantProbe />
      </TenantProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "ai8" }));

    await waitFor(() => expect(replace).toHaveBeenCalledWith("/?tenant=zztest"));
  });

  it("selects a tenant from the menu and closes it from the backdrop", async () => {
    const { container } = render(
      <TenantProvider>
        <TenantSelector />
      </TenantProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: /Tenant: ai8/ }));
    fireEvent.click(screen.getByRole("button", { name: "email4ai" }));
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/?tenant=email4ai"));
    expect(screen.queryByRole("button", { name: "zztest" })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Tenant: ai8/ }));
    const backdrop = container.querySelector(".fixed.inset-0.z-40");
    expect(backdrop).not.toBeNull();
    fireEvent.click(backdrop!);
    expect(screen.queryByRole("button", { name: "zztest" })).not.toBeInTheDocument();
  });

  it("rejects context access outside the provider", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    expect(() => render(<InvalidProbe />)).toThrow("useTenant must be used within a TenantProvider");
    consoleError.mockRestore();
  });
});
