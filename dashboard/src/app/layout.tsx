import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Airborne Dashboard",
  description: "Live activity monitoring for Airborne LLM Gateway",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body className="bg-gray-100 min-h-screen">
        <header className="bg-white border-b border-gray-200 px-6 py-4">
          <h1 className="text-xl font-semibold text-gray-800">Airborne Dashboard</h1>
        </header>
        <main className="p-6">
          {children}
        </main>
      </body>
    </html>
  );
}
