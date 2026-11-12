import type { NextConfig } from "next";

const backendUrl = process.env.BACKEND_URL ?? "http://localhost:3001";

    // Encrypted ballots (~350 KB) and the HE secret key (~260 KB) are posted through server actions
const nextConfig: NextConfig = {
  experimental: {

    serverActions: { bodySizeLimit: "2mb" },
  },
  async rewrites() {
    return [{ source: "/api/:path*", destination: `${backendUrl}/api/:path*` }];
  },
};

export default nextConfig;
