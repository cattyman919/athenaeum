import { getTranslations } from "next-intl/server";
import { ChatWorkspace } from "@/components/chat-workspace";
import { apiServer } from "@/lib/api-server";
import { turnsFromHistory, type ChatTurn } from "@/lib/chat-stream";
import type { ConversationDetail, ConversationPage } from "@/lib/types";

async function getJson<T>(path: string): Promise<T | null> {
  try {
    const res = await apiServer(path);
    return res.ok ? ((await res.json()) as T) : null;
  } catch {
    return null;
  }
}

// Screen 5 — Stitch 70530f16… AI Librarian (docs/07 §7, docs/12 screen 5). The active
// conversation is URL state (`?c=<id>`); `?prefill=` seeds the composer (library/book CTAs).
export default async function ChatPage({
  searchParams,
}: {
  searchParams: Promise<{ c?: string; prefill?: string }>;
}) {
  const sp = await searchParams;
  const activeId = typeof sp.c === "string" && sp.c ? sp.c : undefined;
  const [t, page, detail] = await Promise.all([
    getTranslations("chat"),
    getJson<ConversationPage>("/api/chat/conversations?per_page=50"),
    activeId
      ? getJson<ConversationDetail>(`/api/chat/conversations/${encodeURIComponent(activeId)}`)
      : Promise.resolve(null),
  ]);
  const initialTurns: ChatTurn[] = detail ? turnsFromHistory(detail.messages) : [];

  return (
    <div className="flex flex-col gap-4">
      <header className="flex flex-col gap-1">
        <h1 className="text-3xl font-bold tracking-tight">{t("title")}</h1>
        <p className="text-sm text-muted-foreground">{t("subtitle")}</p>
      </header>
      <ChatWorkspace
        conversations={page?.items ?? []}
        activeId={detail ? detail.id : undefined}
        initialTurns={initialTurns}
        loadError={Boolean(activeId) && !detail}
        prefill={typeof sp.prefill === "string" ? sp.prefill : undefined}
      />
    </div>
  );
}
