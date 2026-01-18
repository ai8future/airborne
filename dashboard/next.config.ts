import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Enable standalone output for Docker
  output: "standalone",
  // Configure environment variables
  env: {
    AIRBORNE_ADMIN_URL: process.env.AIRBORNE_ADMIN_URL || "http://localhost:50054",
  },
};

export default nextConfig;
