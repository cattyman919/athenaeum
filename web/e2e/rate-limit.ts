import { connect } from "node:net";

// The suite shares two seeded accounts (docs/13 §6), so long runs can exceed the general
// 120 req/min per-user bucket (docs/03 §2) — a test-ordering artefact, not app behaviour.
// Request-heavy spec files call this in `beforeAll` to start from fresh counters, the same
// `rl:*` cleanup scripts/e2e-prepare.sh does before the run. Speaks raw RESP so the e2e
// package needs no Redis client dependency.
const REDIS_ADDR = process.env.REDIS_ADDR ?? "localhost:6379";

const SCRIPT =
  "local k = redis.call('KEYS', 'rl:*') for i = 1, #k do redis.call('DEL', k[i]) end return #k";

function encode(args: string[]): string {
  return `*${args.length}\r\n${args.map((a) => `$${Buffer.byteLength(a)}\r\n${a}\r\n`).join("")}`;
}

/** Deletes every rate-limit counter; resolves with the number of keys removed. */
export function resetRateLimits(): Promise<number> {
  const [host, port] = REDIS_ADDR.split(":");
  return new Promise((resolve, reject) => {
    const socket = connect({ host, port: Number(port || 6379) });
    let buf = "";
    socket.setTimeout(5000, () => socket.destroy(new Error("redis timeout")));
    socket.on("connect", () => socket.write(encode(["EVAL", SCRIPT, "0"])));
    socket.on("data", (chunk) => {
      buf += chunk.toString();
      if (!buf.endsWith("\r\n")) return;
      socket.end();
      if (buf.startsWith(":")) resolve(Number(buf.slice(1).trim()));
      else reject(new Error(`redis: ${buf.trim()}`));
    });
    socket.on("error", reject);
  });
}
