"use client";

import { useState } from "react";

const TENANTS = [
  { id: "ai8", label: "ai8" },
  { id: "email4ai", label: "email4ai" },
  { id: "zztest", label: "zztest" },
];

interface TenantSelectorProps {
  onTenantChange?: (tenantId: string) => void;
}

export default function TenantSelector({ onTenantChange }: TenantSelectorProps) {
  const [selectedTenant, setSelectedTenant] = useState("ai8");
  const [showDropdown, setShowDropdown] = useState(false);

  const handleSelect = (tenantId: string) => {
    setSelectedTenant(tenantId);
    setShowDropdown(false);
    onTenantChange?.(tenantId);
  };

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setShowDropdown(!showDropdown)}
        className="flex items-center gap-2 px-3 py-1.5 text-sm font-medium text-gray-600 hover:text-gray-800 hover:bg-gray-100 rounded-lg transition-colors"
      >
        <span className="text-gray-400">Tenant:</span>
        <span className="text-gray-800">{selectedTenant}</span>
        <svg className="w-4 h-4 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
        </svg>
      </button>
      {showDropdown && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setShowDropdown(false)} />
          <div className="absolute right-0 top-full mt-1 bg-white rounded-lg shadow-lg border border-gray-200 py-1 min-w-[120px] z-50">
            {TENANTS.map((tenant) => (
              <button
                key={tenant.id}
                type="button"
                onClick={() => handleSelect(tenant.id)}
                className={`w-full px-3 py-1.5 text-left text-sm hover:bg-gray-100 ${
                  selectedTenant === tenant.id ? "text-blue-600 font-medium" : "text-gray-700"
                }`}
              >
                {tenant.label}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  );
}
