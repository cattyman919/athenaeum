import { defineConfig, devices } from "@playwright/test";

// E2E harness (docs/09 §8.1). `scripts/e2e-prepare.sh` provisions infra + the seeded
// `athenaeum_e2e` database; the servers below are started here. Chromium only in v1.
const WEB_PORT = 13000;
const API_PORT = 18080;
const WEB_URL = `http://localhost:${WEB_PORT}`;
const API_URL = `http://localhost:${API_PORT}`;
// Deterministic AI provider stand-ins (api/cmd/e2e-stubs): OpenAI-style /chat/completions
// for OpenRouter and a fixed-text Whisper /asr (docs/09 §8.1).
const STUBS_PORT = 18090;
const STUBS_URL = `http://localhost:${STUBS_PORT}`;
const DATABASE_URL =
  process.env.E2E_DATABASE_URL ??
  "postgres://athenaeum:athenaeum@localhost:5432/athenaeum_e2e?sslmode=disable";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  timeout: 30_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL: WEB_URL,
    trace: "retain-on-failure",
    launchOptions: {
      // Fake microphone for the voice composer spec (docs/15 §9).
      args: ["--use-fake-device-for-media-stream", "--use-fake-ui-for-media-stream"],
    },
  },
  projects: [
    { name: "setup", testMatch: /auth\.setup\.ts/ },
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
      dependencies: ["setup"],
      testIgnore: /auth\.setup\.ts/,
    },
  ],
  webServer: [
    {
      command: "go run ./cmd/e2e-stubs",
      cwd: "../api",
      url: `${STUBS_URL}/health`,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
      env: { E2E_STUBS_ADDR: `:${STUBS_PORT}` },
    },
    {
      command: "go run ./cmd/api",
      cwd: "../api",
      url: `${API_URL}/api/health`,
      reuseExistingServer: !process.env.CI,
      timeout: 180_000,
      env: {
        APP_ENV: "dev",
        API_PORT: String(API_PORT),
        WEB_ORIGIN: WEB_URL,
        DATABASE_URL,
        REDIS_ADDR: process.env.REDIS_ADDR ?? "localhost:6379",
        JWT_ACCESS_SECRET: "e2e-access-secret-e2e-access-secret",
        JWT_REFRESH_SECRET: "e2e-refresh-secret-e2e-refresh-secret",
        OLLAMA_BASE_URL: process.env.OLLAMA_BASE_URL ?? "http://localhost:11434",
        OFFLINE_MODE: "true",
        // Chat runs the real agent/tools/SSE pipeline against the stub model (docs/01 §7).
        OPENROUTER_API_KEY: "e2e-stub-key",
        OPENROUTER_BASE_URL: STUBS_URL,
        WHISPER_BASE_URL: STUBS_URL,
      },
    },
    {
      command: `pnpm build && pnpm start -p ${WEB_PORT}`,
      url: `${WEB_URL}/login`,
      reuseExistingServer: !process.env.CI,
      timeout: 240_000,
      env: { API_BASE_URL: API_URL },
    },
  ],
});
