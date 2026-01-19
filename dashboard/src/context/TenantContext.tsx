"use client";

import { createContext, useContext, useState, ReactNode } from "react";

const TENANTS = ["ai8", "email4ai", "zztest"] as const;
type TenantId = typeof TENANTS[number];

interface TenantContextType {
  tenant: TenantId;
  setTenant: (tenant: TenantId) => void;
  tenants: readonly typeof TENANTS[number][];
}

const TenantContext = createContext<TenantContextType | null>(null);

export function TenantProvider({ children }: { children: ReactNode }) {
  const [tenant, setTenant] = useState<TenantId>("ai8");

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
