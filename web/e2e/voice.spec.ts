import { expect, test } from "@playwright/test";
import { DEMO_STATE } from "./accounts";
import { gotoReady } from "./helpers";

// Push-to-talk voice input (docs/15 §2, §9). Chromium runs with
// --use-fake-device-for-media-stream --use-fake-ui-for-media-stream (playwright.config.ts),
// so getUserMedia yields a synthetic tone; the API forwards the clip to the api/cmd/e2e-stubs
// Whisper /asr, which always answers with the fixed transcript below.

test.use({ storageState: DEMO_STATE, permissions: ["microphone"] });

const WEB_URL = "http://localhost:13000";
const STUB_TRANSCRIPT = "where can I find books on Babylonian mathematics";

test.describe("voice input", () => {
  test("records, transcribes and inserts the transcript without sending (en)", async ({ page }) => {
    await gotoReady(page, "/chat");
    const mic = page.getByTestId("chat-mic");
    await expect(mic).toBeVisible();
    await expect(mic).toHaveAttribute("aria-label", "Start voice input");

    // Typed text stays; the transcript is inserted beside it.
    await page.getByTestId("chat-composer").fill("Hello,");

    await mic.click();
    await expect(mic).toHaveAttribute("aria-pressed", "true");
    await expect(page.getByTestId("chat-recording")).toContainText("Recording");
    await expect(page.getByTestId("chat-send")).toBeDisabled();
    await page.waitForTimeout(1_200); // capture a short clip of the fake tone

    const transcribe = page.waitForRequest(
      (r) => r.url().endsWith("/api/chat/transcribe") && r.method() === "POST",
    );
    const response = page.waitForResponse((r) => r.url().endsWith("/api/chat/transcribe"));
    await mic.click();
    const req = await transcribe;
    expect(req.headers()["content-type"]).toMatch(/^multipart\/form-data; boundary=/);
    const res = await response;
    expect(res.status()).toBe(200);
    expect((await res.json()).language).toBe("en"); // stub echoes the locale hint

    await expect(page.getByTestId("chat-composer")).toHaveValue(`Hello, ${STUB_TRANSCRIPT}`);
    await expect(page.getByTestId("chat-recording")).toHaveCount(0);
    await expect(mic).toHaveAttribute("aria-pressed", "false");
    await expect(page.getByTestId("chat-voice-note")).toContainText("Review it, then press Send");

    // Voice never auto-submits (docs/15 §2): nothing reached the thread; Send is the user's call.
    await expect(page.getByTestId("chat-turn-item")).toHaveCount(0);
    await expect(page.getByTestId("chat-send")).toBeEnabled();
  });

  test("shows live interim text while recording, then the final transcript (docs/15 §2a)", async ({
    page,
  }) => {
    await gotoReady(page, "/chat");
    const mic = page.getByTestId("chat-mic");
    const composer = page.getByTestId("chat-composer");
    await composer.fill("Hi");

    // Partials go to ?partial=true (the stub's /fast/asr answers a growing strict prefix).
    const partialResponse = page.waitForResponse(
      (r) => r.url().includes("/api/chat/transcribe?partial=true") && r.status() === 200,
    );
    await mic.click();
    await expect(page.getByTestId("chat-recording")).toBeVisible();
    await partialResponse;

    const interim = page.getByTestId("chat-voice-interim");
    await expect(interim).toContainText("where can", { timeout: 8_000 });
    const interimText = (await interim.innerText()).replace(/^.*?:\s*/, "").trim();
    expect(STUB_TRANSCRIPT.startsWith(interimText)).toBe(true);
    expect(interimText).not.toBe(STUB_TRANSCRIPT); // interim is a preview, not the final text
    await expect(interim).toHaveClass(/italic/);
    // Typed text is untouched while interim text is shown; nothing is inserted yet.
    await expect(composer).toHaveValue("Hi");
    await expect(page.getByTestId("chat-recording")).toBeVisible();

    const final = page.waitForResponse((r) => r.url().endsWith("/api/chat/transcribe"));
    await mic.click();
    expect((await final).status()).toBe(200);
    await expect(composer).toHaveValue(`Hi ${STUB_TRANSCRIPT}`);
    await expect(interim).toHaveCount(0);
    await expect(page.getByTestId("chat-turn-item")).toHaveCount(0);
  });

  test("cancelling with Escape discards the interim text and uploads nothing final", async ({
    page,
  }) => {
    await gotoReady(page, "/chat");
    const mic = page.getByTestId("chat-mic");
    let finalPosted = false;
    page.on("request", (r) => {
      if (r.url().endsWith("/api/chat/transcribe")) finalPosted = true;
    });
    await mic.click();
    await expect(page.getByTestId("chat-voice-interim")).toContainText("where can", {
      timeout: 8_000,
    });
    await mic.press("Escape");
    await expect(mic).toHaveAttribute("aria-pressed", "false");
    await expect(page.getByTestId("chat-voice-interim")).toHaveCount(0);
    await expect(page.getByTestId("chat-composer")).toHaveValue("");
    expect(finalPosted).toBe(false);
  });

  test("sends the zh-CN locale as the Whisper hint", async ({ page, context }) => {
    await context.addCookies([{ name: "athenaeum_lang", value: "zh-CN", url: WEB_URL }]);
    await gotoReady(page, "/chat");
    const mic = page.getByTestId("chat-mic");
    await expect(mic).toHaveAttribute("aria-label", "开始语音输入");

    await mic.click();
    await expect(page.getByTestId("chat-recording")).toContainText("正在录音");
    await page.waitForTimeout(1_000);
    const response = page.waitForResponse((r) => r.url().endsWith("/api/chat/transcribe"));
    await mic.click();
    expect((await (await response).json()).language).toBe("zh"); // stub echoes the hint
    await expect(page.getByTestId("chat-composer")).toHaveValue(STUB_TRANSCRIPT);
  });
});

test.describe("voice input without microphone permission", () => {
  test.use({ permissions: [] });

  test("shows the blocked-microphone message and records nothing", async ({ page }) => {
    // --use-fake-ui-for-media-stream auto-accepts prompts, so deny getUserMedia explicitly to
    // exercise the permission-denied path (docs/15 §9).
    await page.addInitScript(() => {
      const md = navigator.mediaDevices;
      if (md) {
        md.getUserMedia = () =>
          Promise.reject(new DOMException("Permission denied", "NotAllowedError"));
      }
    });
    let posted = false;
    page.on("request", (r) => {
      if (r.url().endsWith("/api/chat/transcribe")) posted = true;
    });
    await gotoReady(page, "/chat");
    await page.getByTestId("chat-mic").click();
    await expect(page.getByTestId("chat-voice-note")).toContainText("Microphone access is blocked");
    await expect(page.getByTestId("chat-mic")).toHaveAttribute("aria-pressed", "false");
    await expect(page.getByTestId("chat-recording")).toHaveCount(0);
    expect(posted).toBe(false);
  });

  test("hides the mic when MediaRecorder is unavailable", async ({ page }) => {
    await page.addInitScript(() => {
      // Simulate a browser without MediaRecorder (docs/15 §2: the mic is hidden).
      Reflect.deleteProperty(window, "MediaRecorder");
    });
    await gotoReady(page, "/chat");
    await expect(page.getByTestId("chat-composer")).toBeVisible();
    await expect(page.getByTestId("chat-mic")).toHaveCount(0);
  });
});
