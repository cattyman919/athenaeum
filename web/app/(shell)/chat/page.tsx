import { Suspense } from "react";
import { getTranslations } from "next-intl/server";
import { ChatWorkspace } from "@/components/chat-workspace";
import { Skeleton } from "@/components/ui/skeleton";
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
  const t = await getTranslations("chat");

  return (
    <div className="flex flex-col gap-4">
      <header className="flex flex-col gap-1">
        <h1 className="text-3xl font-bold tracking-tight">{t("title")}</h1>
        <p className="text-sm text-muted-foreground">{t("subtitle")}</p>
      </header>
      {/* In-page Suspense skeleton (docs/07 §9; no route-level loading.tsx). */}
      <Suspense fallback={<ChatSkeleton />}>
        <ChatContent
          activeId={activeId}
          prefill={typeof sp.prefill === "string" ? sp.prefill : undefined}
        />
      </Suspense>
    </div>
  );
}

async function ChatContent({ activeId, prefill }: { activeId?: string; prefill?: string }) {
  const [page, detail] = await Promise.all([
    getJson<ConversationPage>("/api/chat/conversations?per_page=50"),
    activeId
      ? getJson<ConversationDetail>(`/api/chat/conversations/${encodeURIComponent(activeId)}`)
      : Promise.resolve(null),
  ]);
  const initialTurns: ChatTurn[] = detail ? turnsFromHistory(detail.messages) : [];
  return (
    <ChatWorkspace
      conversations={page?.items ?? []}
      activeId={detail ? detail.id : undefined}
      initialTurns={initialTurns}
      loadError={Boolean(activeId) && !detail}
      listError={!page}
      prefill={prefill}
    />
  );
}

function ChatSkeleton() {
  return (
    <div
      aria-busy="true"
      data-testid="chat-skeleton"
      className="flex h-[calc(100vh-12rem)] min-h-[34rem] overflow-hidden rounded-lg border border-border bg-card shadow-sm"
    >
      <div className="hidden w-72 shrink-0 flex-col gap-2 border-r border-border p-3 md:flex">
        <Skeleton className="h-8 w-full" />
        {Array.from({ length: 5 }).map((_, i) => (
          <Skeleton key={i} className="h-10 w-full" />
        ))}
      </div>
      <div className="flex flex-1 flex-col gap-4 p-6">
        <Skeleton className="h-6 w-1/2" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-24 w-3/4" />
        <div className="mt-auto">
          <Skeleton className="h-20 w-full" />
        </div>
      </div>
    </div>
  );
}
