import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Enable standalone output for Docker
  output: "standalone",
  // Playwright's local server probes use this loopback host in CI and locally.
  allowedDevOrigins: ["127.0.0.1"],
};

export default nextConfig;
