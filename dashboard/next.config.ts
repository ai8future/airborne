import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Enable standalone output for Docker
  output: "standalone",
  // Playwright's local server probes use this loopback host in CI and locally.
  allowedDevOrigins: ["127.0.0.1"],
  // Configure environment variables
  env: {
    AIRBORNE_ADMIN_URL: process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054",
  },
};

export default nextConfig;
