// Incremental Server-Sent Events parser for fetch + ReadableStream (docs/07 §6).
// `EventSource` cannot POST, so the chat client feeds decoded chunks here. Frames are
// separated by a blank line; `event:` names the frame, `data:` lines are joined with
// "\n", and comment lines (": ping" heartbeats — docs/06 §6) are ignored.

export interface SSEFrame {
  event: string;
  data: string;
}

export interface SSEParser {
  /** Feeds one decoded chunk; returns every frame completed by it. */
  push(chunk: string): SSEFrame[];
  /** Flushes a trailing frame that was not terminated by a blank line. */
  end(): SSEFrame[];
}

export function createSSEParser(): SSEParser {
  let buf = "";
  let pendingCR = "";

  const parseFrame = (raw: string): SSEFrame | null => {
    let event = "message";
    const data: string[] = [];
    for (const line of raw.split("\n")) {
      if (line === "" || line.startsWith(":")) continue;
      const i = line.indexOf(":");
      const field = i < 0 ? line : line.slice(0, i);
      let value = i < 0 ? "" : line.slice(i + 1);
      if (value.startsWith(" ")) value = value.slice(1);
      if (field === "event") event = value;
      else if (field === "data") data.push(value);
    }
    return data.length ? { event, data: data.join("\n") } : null;
  };

  const drain = (final: boolean): SSEFrame[] => {
    const out: SSEFrame[] = [];
    let sep: number;
    while ((sep = buf.indexOf("\n\n")) >= 0) {
      const frame = parseFrame(buf.slice(0, sep));
      buf = buf.slice(sep + 2);
      if (frame) out.push(frame);
    }
    if (final && buf.trim()) {
      const frame = parseFrame(buf);
      if (frame) out.push(frame);
      buf = "";
    }
    return out;
  };

  return {
    push(chunk) {
      // A CR at the chunk edge may be the first half of a CRLF; hold it back.
      let text = pendingCR + chunk;
      pendingCR = "";
      if (text.endsWith("\r")) {
        pendingCR = "\r";
        text = text.slice(0, -1);
      }
      buf += text.replace(/\r\n?/g, "\n");
      return drain(false);
    },
    end() {
      if (pendingCR) buf += "\n";
      pendingCR = "";
      return drain(true);
    },
  };
}
