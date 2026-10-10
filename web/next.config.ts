import type { NextConfig } from "next";
import createNextIntlPlugin from "next-intl/plugin";

const withNextIntl = createNextIntlPlugin("./i18n/request.ts");

// `web` proxies /api/* to the Go API so browsers stay same-origin (docs/01 §6).
// Rewrites are baked at build time; the Dockerfile passes API_BASE_URL as a build arg.
const apiBase = process.env.API_BASE_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  reactStrictMode: true,
  // Compression would buffer the SSE chat stream.
  compress: false,
  // The repo root also has a lockfile (commitlint tooling); pin tracing to web/.
  outputFileTracingRoot: process.cwd(),
  // Ship the ZXing-C++ reader binary for the barcode-detector ponyfill as a static asset
  // (docs/11 §3) rather than letting zxing-wasm fetch it from jsDelivr at runtime.
  webpack(config) {
    config.module.rules.push({
      test: /zxing_reader\.wasm$/,
      type: "asset/resource",
      generator: { filename: "static/media/[name].[contenthash:8][ext]" },
    });
    return config;
  },
  async rewrites() {
    return [{ source: "/api/:path*", destination: `${apiBase}/api/:path*` }];
  },
};

export default withNextIntl(nextConfig);
