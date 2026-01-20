"use client";

import { createContext, useContext, useState, useEffect, useCallback, ReactNode } from "react";
import { useSearchParams, useRouter, usePathname } from "next/navigation";

const TENANTS = ["ai8", "email4ai", "zztest"] as const;
type TenantId = typeof TENANTS[number];

interface TenantContextType {
  tenant: TenantId;
  setTenant: (tenant: TenantId) => void;
  tenants: readonly typeof TENANTS[number][];
}

const TenantContext = createContext<TenantContextType | null>(null);

function isValidTenant(value: string | null): value is TenantId {
  return value !== null && TENANTS.includes(value as TenantId);
}

export function TenantProvider({ children }: { children: ReactNode }) {
  const searchParams = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();

  // Initialize from URL or default to "ai8"
  const urlTenant = searchParams.get("tenant");
  const initialTenant = isValidTenant(urlTenant) ? urlTenant : "ai8";
  const [tenant, setTenantState] = useState<TenantId>(initialTenant);

  // Sync state with URL on mount (in case URL changed externally)
  useEffect(() => {
    const urlTenant = searchParams.get("tenant");
    if (isValidTenant(urlTenant) && urlTenant !== tenant) {
      setTenantState(urlTenant);
    }
  }, [searchParams, tenant]);

  // Update URL when tenant changes
  const setTenant = useCallback((newTenant: TenantId) => {
    setTenantState(newTenant);

    // Update URL with new tenant
    const params = new URLSearchParams(searchParams.toString());
    params.set("tenant", newTenant);
    router.replace(`${pathname}?${params.toString()}`);
  }, [searchParams, router, pathname]);

  return (
    <TenantContext.Provider value={{ tenant, setTenant, tenants: TENANTS }}>
      {children}
    </TenantContext.Provider>
  );
}

export function useTenant() {
  const context = useContext(TenantContext);
  if (!context) {
    throw new Error("useTenant must be used within a TenantProvider");
  }
  return context;
}
