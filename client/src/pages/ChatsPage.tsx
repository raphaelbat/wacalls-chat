import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { ArrowDownNarrowWide, ArrowUpNarrowWide, CheckCheck, Eye, Filter, ListFilter, MoreVertical, Plus, XCircle } from "lucide-react";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import { AppShell } from "@/components/layout/AppShell";
import { ensureSessionsWired, useSessions } from "@/stores/sessions";
import {
  ensureChatsWired,
  fetchChats,
  fetchMessages,
  markChatAsRead,
  setActiveChat,
  setChatStatus,
  useChats,
} from "@/stores/chats";
import type { ChatSummary } from "@/types/chat";
import { ChatList, filterChats } from "@/components/domain/chat/ChatList";
import { ChatView } from "@/components/domain/chat/ChatView";
import { isGroupJid } from "@/components/domain/chat/format";
import { useAuth } from "@/stores/auth";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import { closeChat } from "@/services/chats";
import { NewChatDialog } from "@/components/domain/chat/NewChatDialog";

type Tab = "open" | "waiting" | "group";

const EMPTY_CHATS: ChatSummary[] = [];

export const ChatsPage = () => {
  const { t } = useTranslation();
  ensureSessionsWired();
  ensureChatsWired();

  const sessions = useSessions((s) => s.sessions);
  const activeId = useSessions((s) => s.activeId);
  const [pickedSession, setPickedSession] = useState<string | null>(activeId);
  const [searchParams, setSearchParams] = useSearchParams();

  // Hidrata pickedSession quando a store de sessões resolve depois do primeiro
  // render (acontece em F5: na primeira renderização activeId ainda é null e
  // a tela ficava presa no estado "Pareie uma sessão...").
  useEffect(() => {
    if (pickedSession) return;
    if (activeId) {
      setPickedSession(activeId);
      return;
    }
    const firstPaired = sessions.find((s) => s.paired);
    if (firstPaired) setPickedSession(firstPaired.id);
  }, [activeId, sessions, pickedSession]);

  // Deep-link entry point: /chats?sid=...&jid=... opens that conversation
  // directly (used by the dedicated /contacts page). The params are consumed
  // once and then cleared so refreshes don't re-fire the navigation.
  useEffect(() => {
    const sid = searchParams.get("sid");
    const jid = searchParams.get("jid");
    if (!sid && !jid) return;
    if (sid) setPickedSession(sid);
    if (sid && jid) setActiveChat(sid, jid);
    const next = new URLSearchParams(searchParams);
    next.delete("sid");
    next.delete("jid");
    setSearchParams(next, { replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const sessionId =
    pickedSession ?? activeId ?? sessions.find((s) => s.paired)?.id ?? null;
  const activeJid = useChats((s) => (sessionId ? s.activeJidBySession[sessionId] ?? null : null));
  const [tab, setTab] = useState<Tab>("waiting");
  const me = useAuth((s) => s.user);
  const chats = useChats((s) => (sessionId ? s.chatsBySession[sessionId] ?? EMPTY_CHATS : EMPTY_CHATS));

  const [unreadOnly, setUnreadOnly] = useState(false);
  const [sort, setSort] = useState<"desc" | "asc">("desc");
  const [confirmCloseAll, setConfirmCloseAll] = useState(false);
  const [bulkBusy, setBulkBusy] = useState(false);
  const [newChatOpen, setNewChatOpen] = useState(false);

  useEffect(() => {
    if (sessionId) void fetchChats(sessionId);
  }, [sessionId]);

  // Refetch quando o usuário volta para a aba ou re-foca a janela.
  // Cobre o caso de "abro a página e está vazio, atualizo e aparece":
  // se o whatsmeow demorou para sincronizar, o foco re-dispara a busca
  // sem precisar de F5 manual.
  useEffect(() => {
    if (!sessionId) return;
    const refetch = () => {
      if (document.visibilityState === "visible") void fetchChats(sessionId);
    };
    window.addEventListener("focus", refetch);
    document.addEventListener("visibilitychange", refetch);
    return () => {
      window.removeEventListener("focus", refetch);
      document.removeEventListener("visibilitychange", refetch);
    };
  }, [sessionId]);

  useEffect(() => {
    if (sessionId && activeJid) {
      void fetchMessages(sessionId, activeJid);
      markChatAsRead(sessionId, activeJid);
    }
  }, [sessionId, activeJid]);

  // Follow real-time status changes of the active chat (SSE chat-meta).
  // If another agent assigns / requeues / closes it, hop tabs automatically
  // so the conversation stays visible without a manual refresh.
  const activeChat = useMemo(
    () => (activeJid ? chats.find((c) => c.chatJid === activeJid) ?? null : null),
    [chats, activeJid],
  );
  const activeStatus = activeChat?.status;
  const activeIsGroup = activeChat ? activeChat.isGroup || isGroupJid(activeChat.chatJid) : false;
  useEffect(() => {
    if (!activeChat) return;
    if (activeIsGroup) {
      if (tab !== "group") setTab("group");
      return;
    }
    if (activeStatus === "open" && tab !== "open") setTab("open");
    else if ((activeStatus === "waiting" || activeStatus === "closed") && tab !== "waiting") setTab("waiting");
    // tab intentionally omitted: we only react to status flips, not user tab clicks.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeStatus, activeIsGroup, activeChat?.chatJid]);

  const tabCounts = useMemo(() => {
    const counts = { open: 0, waiting: 0, group: 0 };
    for (const c of chats) {
      const isGroup = c.isGroup || isGroupJid(c.chatJid);
      // Count tickets (conversations) per tab, not unread messages — the unread
      // badge already lives inside each ticket card. Grupos "fechados" não
      // devem contar no badge, senão o número persiste após "Finalizar".
      if (isGroup) {
        const status = c.status ?? "group";
        if (status !== "closed") counts.group += 1;
      }
      else if ((c.status ?? "waiting") === "waiting") counts.waiting += 1;
      else if ((c.status ?? "") === "open" && (!me?.id || !c.assignedUserId || c.assignedUserId === me.id))
        counts.open += 1;
    }
    return counts;
  }, [chats, me?.id]);

  const pairedSessions = useMemo(() => sessions.filter((s) => s.paired), [sessions]);

  // Conversations targeted by "Fechar todos da aba" — respects current filters.
  const targetedForBulk = useMemo(
    () => filterChats(chats, tab, me?.id ?? null, unreadOnly),
    [chats, tab, me?.id, unreadOnly],
  );

  const TAB_LABEL: Record<Tab, string> = {
    open: t("pages.chats.tabs.open"),
    waiting: t("pages.chats.tabs.waiting"),
    group: t("pages.chats.tabs.group"),
  };

  const handleBulkClose = async () => {
    if (!sessionId || targetedForBulk.length === 0) return;
    setBulkBusy(true);
    const total = targetedForBulk.length;
    const tId = toast.loading(t("pages.chats.bulkLoading", { count: total }));
    // Close in parallel (limited concurrency) so 99+ chats don't take minutes.
    const queue = [...targetedForBulk];
    let ok = 0;
    let fail = 0;
    const worker = async () => {
      while (queue.length) {
        const c = queue.shift();
        if (!c) break;
        try {
          await closeChat(sessionId, c.chatJid, "encerramento em massa");
          // Optimistic removal — the SSE chat-meta update will confirm shortly.
          setChatStatus(sessionId, c.chatJid, "closed", null);
          ok += 1;
        } catch {
          fail += 1;
        }
      }
    };
    await Promise.all(Array.from({ length: Math.min(6, targetedForBulk.length) }, worker));
    setBulkBusy(false);
    toast.dismiss(tId);
    if (fail === 0) toast.success(t("pages.chats.bulkSuccess", { count: ok }));
    else toast.error(t("pages.chats.bulkPartial", { ok, fail }));
    void fetchChats(sessionId);
  };

  if (!sessionId) {
    return (
      <AppShell>
        <div className="grid h-full place-items-center text-sm text-muted-foreground">
          {t("pages.chats.pairSessionPrompt")}
        </div>
      </AppShell>
    );
  }

  return (
    <AppShell>
      <div className="flex h-full min-h-0 gap-3">
        <div className="flex w-96 shrink-0 flex-col overflow-hidden rounded-2xl border bg-card shadow-sm">
          {pairedSessions.length > 1 && (
            <div className="border-b p-2">
              <select
                className="w-full rounded-md border bg-background px-2 py-1.5 text-sm"
                value={sessionId}
                onChange={(e) => {
                  setPickedSession(e.target.value);
                  setActiveChat(e.target.value, null);
                }}
              >
                {pairedSessions.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
              </select>
            </div>
          )}
          <div className="flex items-stretch border-b text-xs font-medium">
            <div className="flex flex-1">
            {([
              { id: "open", label: t("pages.chats.tabs.open") },
              { id: "waiting", label: t("pages.chats.tabs.waiting") },
              { id: "group", label: t("pages.chats.tabs.group") },
            ] as { id: Tab; label: string }[]).map((t) => {
              const count = tabCounts[t.id];
              return (
                <button
                  key={t.id}
                  onClick={() => setTab(t.id)}
                  className={`flex flex-1 items-center justify-center gap-1.5 px-2 py-2 ${tab === t.id ? "border-b-2 border-primary text-foreground" : "text-muted-foreground hover:bg-muted/50"}`}
                >
                  <span>{t.label}</span>
                  {count > 0 && (
                    <span className="grid h-4 min-w-[1rem] place-items-center rounded-full bg-primary px-1 text-[10px] font-semibold leading-none text-primary-foreground">
                      {count > 99 ? "99+" : count}
                    </span>
                  )}
                </button>
              );
            })}
            </div>
            <DropdownMenu>
              <button
                type="button"
                onClick={() => setNewChatOpen(true)}
                aria-label="Abrir atendimento"
                title="Abrir atendimento"
                className="grid w-9 shrink-0 place-items-center border-l text-muted-foreground hover:bg-muted/50 hover:text-foreground"
              >
                <Plus className="h-4 w-4" />
              </button>
              <DropdownMenuTrigger
                aria-label={t("pages.chats.listOptionsAria")}
                className="grid w-9 shrink-0 place-items-center border-l text-muted-foreground hover:bg-muted/50 hover:text-foreground"
              >
                <MoreVertical className="h-4 w-4" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-56">
                <DropdownMenuLabel className="text-xs uppercase tracking-wide text-muted-foreground">
                  {t("actions.filter")}
                </DropdownMenuLabel>
                <DropdownMenuItem onSelect={() => setUnreadOnly(false)} className="gap-2">
                  <Eye className="h-4 w-4" /> {t("actions.viewAll")}
                  {!unreadOnly && <CheckCheck className="ml-auto h-3.5 w-3.5 text-primary" />}
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => setUnreadOnly(true)} className="gap-2">
                  <Filter className="h-4 w-4" /> {t("actions.unreadOnly")}
                  {unreadOnly && <CheckCheck className="ml-auto h-3.5 w-3.5 text-primary" />}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuLabel className="text-xs uppercase tracking-wide text-muted-foreground">
                  {t("actions.sort")}
                </DropdownMenuLabel>
                <DropdownMenuItem onSelect={() => setSort("desc")} className="gap-2">
                  <ArrowDownNarrowWide className="h-4 w-4" /> {t("actions.sortNewest")}
                  {sort === "desc" && <CheckCheck className="ml-auto h-3.5 w-3.5 text-primary" />}
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => setSort("asc")} className="gap-2">
                  <ArrowUpNarrowWide className="h-4 w-4" /> {t("actions.sortOldest")}
                  {sort === "asc" && <CheckCheck className="ml-auto h-3.5 w-3.5 text-primary" />}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  disabled={bulkBusy || targetedForBulk.length === 0}
                  onSelect={() => setConfirmCloseAll(true)}
                  className="gap-2 text-destructive focus:text-destructive"
                >
                  <XCircle className="h-4 w-4" />
                  {t("pages.chats.bulkCount", { count: targetedForBulk.length })}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
          {(unreadOnly || sort === "asc") && (
            <div className="flex items-center gap-2 border-b bg-muted/30 px-3 py-1.5 text-[11px] text-muted-foreground">
              <ListFilter className="h-3 w-3" />
              <span className="truncate">
                {unreadOnly ? t("pages.chats.filterUnreadHint") : ""}
                {unreadOnly && sort === "asc" ? " · " : ""}
                {sort === "asc" ? t("pages.chats.filterOldestHint") : ""}
              </span>
              <button
                type="button"
                onClick={() => {
                  setUnreadOnly(false);
                  setSort("desc");
                }}
                className="ml-auto text-[10px] uppercase tracking-wider text-primary hover:underline"
              >
                {t("actions.clear")}
              </button>
            </div>
          )}
          <ChatList
            sessionId={sessionId}
            activeJid={activeJid}
            tab={tab}
            myId={me?.id ?? null}
            unreadOnly={unreadOnly}
            sort={sort}
            onSelect={(jid) => setActiveChat(sessionId, jid)}
            onStatusChange={(status) => {
              if (status === "open") setTab("open");
              else if (status === "waiting" || status === "closed") setTab("waiting");
            }}
          />
        </div>
        <div className="flex min-w-0 flex-1 overflow-hidden rounded-2xl border bg-card shadow-sm">
          <ChatView
            sessionId={sessionId}
            chatJid={activeJid}
            onStatusChange={(status) => {
              if (status === "open") setTab("open");
              else if (status === "closed" || status === "waiting") setTab("waiting");
            }}
          />
        </div>
      </div>
      <ConfirmDialog
        open={confirmCloseAll}
        onOpenChange={setConfirmCloseAll}
        title={t("pages.chats.closeAllTitle", { tab: TAB_LABEL[tab] })}
        description={t("pages.chats.closeAllDescription", {
          count: targetedForBulk.length,
          unreadHint: unreadOnly ? t("pages.chats.closeAllDescription_unread") : "",
        })}
        confirmLabel={t("actions.closeAll")}
        destructive
        onConfirm={handleBulkClose}
      />
      <NewChatDialog
        open={newChatOpen}
        onOpenChange={setNewChatOpen}
        sessionId={sessionId}
        onOpened={() => {
          // Após criar/abrir, alterna para a aba aguardando para revelar o ticket.
          setTab("waiting");
        }}
      />
    </AppShell>
  );
};