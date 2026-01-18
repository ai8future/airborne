import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Configure environment variables
  env: {
    AIRBORNE_ADMIN_URL: process.env.AIRBORNE_ADMIN_URL || "http://localhost:50052",
  },
};

export default nextConfig;
