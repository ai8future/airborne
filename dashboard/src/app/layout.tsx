import type { Metadata } from "next";
import "./globals.css";
import TenantSelector from "@/components/TenantSelector";
import { TenantProvider } from "@/context/TenantContext";

export const metadata: Metadata = {
  title: "Airborne",
  description: "Live activity monitoring for Airborne LLM Gateway",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" className="h-full">
      <body className="h-full flex flex-col overflow-hidden bg-gray-100">
        <TenantProvider>
          <header className="flex-shrink-0 bg-white border-b border-gray-200 px-6 py-4 flex items-center justify-between">
            <h1 className="text-xl font-semibold text-gray-800">Airborne</h1>
            <TenantSelector />
          </header>
          <main className="flex-1 overflow-hidden p-6 pb-24">
            {children}
          </main>
        </TenantProvider>
      </body>
    </html>
  );
}
