import type { NextConfig } from "next";
import createNextIntlPlugin from "next-intl/plugin";

const withNextIntl = createNextIntlPlugin("./i18n/request.ts");

// `web` proxies /api/* to the Go API so browsers stay same-origin (docs/01 §6).
// Rewrites are baked at build time; the Dockerfile passes API_BASE_URL as a build arg.
const apiBase = process.env.API_BASE_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  reactStrictMode: true,
  // The repo root also has a lockfile (commitlint tooling); pin tracing to web/.
  outputFileTracingRoot: process.cwd(),
  // Compression would buffer the SSE chat stream.
  compress: false,
  async rewrites() {
    return [{ source: "/api/:path*", destination: `${apiBase}/api/:path*` }];
  },
};

export default withNextIntl(nextConfig);
