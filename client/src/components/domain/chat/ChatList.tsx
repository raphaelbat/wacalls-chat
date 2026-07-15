import { useMemo, useState } from "react";
import { ArrowLeftRight, Check, RotateCcw, Send, X } from "lucide-react";
import { toast } from "sonner";
import { setChatStatus, useChats } from "@/stores/chats";
import { useSessions } from "@/stores/sessions";
import type { ChatSummary } from "@/types/chat";
import { formatPeer, formatRelative, isGroupJid, previewBody } from "./format";
import { assignChat, closeChat, requeueChat } from "@/services/chats";
import { TransferDialog } from "./TransferDialog";
import { useAuth } from "@/stores/auth";

import { CloseReasonDialog } from "./CloseReasonDialog";

const EMPTY: ChatSummary[] = [];

export type ChatTab = "open" | "waiting" | "group";

interface Props {
  sessionId: string;
  activeJid: string | null;
  tab: ChatTab;
  myId: string | null;
  unreadOnly?: boolean;
  sort?: "desc" | "asc";
  onSelect: (jid: string) => void;
  onStatusChange?: (status: "open" | "waiting" | "closed") => void;
}

// filterChats applies the tab + extra filters that the dropdown menu controls.
// Exported so the parent page can pre-compute which chats a bulk action covers.
export const filterChats = (
  chats: ChatSummary[],
  tab: ChatTab,
  myId: string | null,
  unreadOnly = false,
): ChatSummary[] =>
  chats.filter((c) => {
    const isGroup = c.isGroup || isGroupJid(c.chatJid);
    const status = c.status ?? (c.lastFromMe ? "open" : "waiting");
    // Conversations that were closed in bulk must vanish from every tab
    // (including Grupos) until a new message arrives and reopens them.
    if (status === "closed") return false;
    if (tab === "group") {
      if (!isGroup) return false;
    } else {
      if (isGroup) return false;
      if (tab === "waiting" && status !== "waiting") return false;
      if (tab === "open") {
        if (status !== "open") return false;
        if (myId && c.assignedUserId && c.assignedUserId !== myId) return false;
      }
    }
    if (unreadOnly && (c.unread ?? 0) <= 0) return false;
    return true;
  });

export const ChatList = ({ sessionId, activeJid, tab, myId, unreadOnly, sort = "desc", onSelect, onStatusChange }: Props) => {
  const chats = useChats((s) => s.chatsBySession[sessionId] ?? EMPTY);
  const loading = useChats((s) => s.loadingChats[sessionId] ?? false);
  const sessionName = useSessions((s) => s.sessions.find((x) => x.id === sessionId)?.name ?? "");
  const [transferFor, setTransferFor] = useState<ChatSummary | null>(null);
  const [closeFor, setCloseFor] = useState<ChatSummary | null>(null);
  const filtered = useMemo(() => {
    const list = filterChats(chats, tab, myId, unreadOnly);
    return [...list].sort((a, b) =>
      sort === "asc" ? (a.lastTs ?? 0) - (b.lastTs ?? 0) : (b.lastTs ?? 0) - (a.lastTs ?? 0),
    );
  }, [chats, tab, myId, unreadOnly, sort]);

  if (loading && filtered.length === 0) {
    return <div className="p-4 text-sm text-muted-foreground">Carregando conversas…</div>;
  }

  if (filtered.length === 0) {
    return (
      <div className="p-4 text-sm text-muted-foreground">
        Nada por aqui ainda.
      </div>
    );
  }

  return (
    <>
      <ul className="scrollbar-thin flex-1 overflow-y-auto">
        {filtered.map((c) => (
          <ChatRow
            key={c.chatJid}
            chat={c}
            sessionId={sessionId}
            sessionName={sessionName}
            active={c.chatJid === activeJid}
            tab={tab}
            onClick={() => onSelect(c.chatJid)}
            onTransfer={() => setTransferFor(c)}
            onRequestClose={() => setCloseFor(c)}
            onStatusChange={onStatusChange}
          />
        ))}
      </ul>
      {closeFor && (
        <CloseReasonDialog
          chatName={closeFor.name?.trim() || formatPeer(closeFor.chatJid)}
          onCancel={() => setCloseFor(null)}
          onConfirm={async (reason) => {
            const target = closeFor;
            setCloseFor(null);
            try {
              await closeChat(sessionId, target.chatJid, reason);
              setChatStatus(sessionId, target.chatJid, "closed", null);
              onStatusChange?.("closed");
              toast.success("Atendimento finalizado");
            } catch (err) {
              toast.error(err instanceof Error ? err.message : "Falha ao finalizar");
            }
          }}
        />
      )}
      {transferFor && (
        <TransferDialog
          open={!!transferFor}
          onOpenChange={(o) => { if (!o) setTransferFor(null); }}
          sessionId={sessionId}
          chatJid={transferFor.chatJid}
          chatName={transferFor.name?.trim() || formatPeer(transferFor.chatJid)}
          excludeUserId={useAuth.getState().user?.id ?? null}
          onTransferred={() => {
            setChatStatus(sessionId, transferFor.chatJid, "open");
            onStatusChange?.("open");
          }}
        />
      )}
    </>
  );
};

interface RowProps {
  chat: ChatSummary;
  sessionId: string;
  sessionName: string;
  active: boolean;
  tab: ChatTab;
  onClick: () => void;
  onTransfer: () => void;
  onRequestClose: () => void;
  onStatusChange?: (status: "open" | "waiting" | "closed") => void;
}

const ChatRow = ({ chat, sessionId, sessionName, active, tab, onClick, onTransfer, onRequestClose, onStatusChange }: RowProps) => {
  const name = chat.name && chat.name.trim() !== "" ? chat.name : formatPeer(chat.chatJid);
  const unread = chat.unread ?? 0;
  const isGroup = chat.isGroup || isGroupJid(chat.chatJid);
  const me = useAuth((s) => s.user);
  
  const [busy, setBusy] = useState<null | "assign" | "close" | "requeue" | "transfer">(null);
  const run = async (kind: typeof busy, fn: () => Promise<void>) => {
    if (busy) return;
    setBusy(kind);
    try { await fn(); } finally { setBusy(null); }
  };

  // Action handlers. Each one calls the existing API and optimistically
  // mutates the local chat store so the row hops to the right tab instantly.
  const handleAssign = (e: React.MouseEvent) => {
    e.stopPropagation();
    void run("assign", async () => {
      try {
        await assignChat(sessionId, chat.chatJid);
        setChatStatus(sessionId, chat.chatJid, "open", me?.id ?? null);
        onStatusChange?.("open");
        // Abre automaticamente o ticket recém-aceito.
        onClick();
        toast.success("Atendimento aceito");
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Falha ao aceitar");
      }
    });
  };
  const handleClose = (e: React.MouseEvent) => {
    e.stopPropagation();
    void run("close", async () => {
      try {
        await closeChat(sessionId, chat.chatJid);
        setChatStatus(sessionId, chat.chatJid, "closed", null);
        onStatusChange?.("closed");
        toast.success("Atendimento finalizado");
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Falha ao finalizar");
      }
    });
  };
  const handleRequeue = (e: React.MouseEvent) => {
    e.stopPropagation();
    void run("requeue", async () => {
      try {
        await requeueChat(sessionId, chat.chatJid);
        setChatStatus(sessionId, chat.chatJid, "waiting", null);
        onStatusChange?.("waiting");
        toast.success("Devolvido para a fila");
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Falha ao devolver");
      }
    });
  };
  const handleTransfer = (e: React.MouseEvent) => {
    e.stopPropagation();
    onTransfer();
  };

  return (
    <li>
      <div
        onClick={onClick}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") onClick();
        }}
        className={`group flex w-full cursor-pointer items-start gap-3 border-b pl-3 pr-4 py-3 text-left transition-colors hover:bg-muted/60 ${active ? "bg-muted" : ""}`}
      >
        {/* Avatar with a small WhatsApp channel badge in the bottom-right,
            matching the reference: the green circle marks the origin even
            when the connection pill is not visible. */}
        <div className="relative shrink-0">
          <span className="relative grid h-11 w-11 place-items-center overflow-hidden rounded-full bg-primary/10 text-sm font-semibold text-primary">
            {/* Letra de fallback sempre renderiza por baixo, então o card
                aparece imediatamente; a foto, quando decodificar, faz
                fade-in por cima — sem o "buraco" branco que parecia delay. */}
            <span aria-hidden className="select-none">{name.slice(0, 1).toUpperCase()}</span>
            {chat.avatarUrl && (
              <img
                src={chat.avatarUrl}
                alt={name}
                width={44}
                height={44}
                loading="lazy"
                decoding="async"
                className="absolute inset-0 h-full w-full object-cover opacity-0 transition-opacity duration-150"
                onLoad={(e) => {
                  (e.currentTarget as HTMLImageElement).style.opacity = "1";
                }}
                onError={(e) => {
                  (e.currentTarget as HTMLImageElement).style.display = "none";
                }}
              />
            )}
          </span>
          <span
            aria-label="WhatsApp"
            title="WhatsApp"
            className="absolute -bottom-0.5 -right-0.5 grid h-4 w-4 place-items-center rounded-full bg-[#25D366] text-white ring-2 ring-background"
          >
            <svg viewBox="0 0 32 32" className="h-2.5 w-2.5" fill="currentColor" aria-hidden>
              <path d="M19.11 17.27c-.28-.14-1.65-.81-1.9-.9-.26-.1-.45-.14-.63.14-.19.28-.72.9-.88 1.08-.16.19-.32.21-.6.07-.28-.14-1.17-.43-2.23-1.37-.82-.73-1.38-1.63-1.54-1.91-.16-.28-.02-.43.12-.57.13-.13.28-.32.42-.49.14-.17.19-.28.28-.47.09-.19.05-.35-.02-.49-.07-.14-.63-1.52-.86-2.08-.23-.55-.46-.47-.63-.48l-.54-.01c-.19 0-.49.07-.74.35-.26.28-.97.95-.97 2.32 0 1.37 1 2.69 1.14 2.88.14.19 1.96 3 4.75 4.21.66.29 1.18.46 1.58.59.66.21 1.26.18 1.74.11.53-.08 1.65-.67 1.88-1.32.23-.65.23-1.21.16-1.32-.07-.11-.26-.18-.54-.32z M16.02 4C9.4 4 4.04 9.36 4.04 15.98c0 2.12.55 4.12 1.6 5.91L4 28l6.27-1.64a11.94 11.94 0 0 0 5.74 1.46h.01c6.62 0 11.98-5.36 11.98-11.98C28 9.36 22.64 4 16.02 4zm0 21.84h-.01a9.86 9.86 0 0 1-5.02-1.37l-.36-.21-3.72.97.99-3.63-.23-.37a9.86 9.86 0 0 1-1.51-5.25c0-5.45 4.43-9.88 9.88-9.88 2.64 0 5.12 1.03 6.98 2.9a9.81 9.81 0 0 1 2.89 6.99c0 5.45-4.43 9.88-9.89 9.88z"/>
            </svg>
          </span>
          {unread > 0 && (
            <span
              aria-label={`${unread} não lida${unread === 1 ? "" : "s"}`}
              className="absolute -top-1 -right-1 grid h-5 min-w-[1.25rem] place-items-center rounded-full bg-primary px-1.5 text-[10px] font-semibold leading-none text-primary-foreground ring-2 ring-background"
            >
              {unread > 99 ? "99+" : unread}
            </span>
          )}
        </div>

        <div className="min-w-0 flex-1">
          <div className="flex items-center justify-between gap-2">
            <span className={`min-w-0 truncate text-sm ${unread > 0 ? "font-semibold text-foreground" : "font-medium text-foreground"}`}>
              {name}
            </span>
            {sessionName && (
              <span className="inline-flex items-center gap-1 rounded-full border border-emerald-500/40 bg-emerald-500/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-emerald-600 dark:text-emerald-400">
                <Send className="h-2.5 w-2.5" />
                {sessionName}
              </span>
            )}
          </div>

          <div className="mt-1 flex items-center gap-2 text-[10px] text-muted-foreground">
            <span>{formatRelative(chat.lastTs)}</span>
          </div>

          <div className="mt-1 flex items-end justify-between gap-2">
            <span className={`min-w-0 flex-1 truncate text-xs ${unread > 0 ? "text-foreground" : "text-muted-foreground"}`}>
              {chat.lastFromMe ? "Você: " : ""}
              {previewBody(chat.lastKind, chat.lastMessage)}
            </span>
          </div>
        </div>

        {/* Side action column — buttons stack to the right of the content,
            matching the reference layout (Atendendo/Aguardando cards). */}
        {!isGroup && (tab === "waiting" || tab === "open") && (
          <div className="ml-3 mr-1 flex shrink-0 items-center justify-center gap-1 self-center">
            {tab === "waiting" ? (
              <>
                {/* Aguardando: aceitar / transferir / finalizar — todos alinhados em linha */}
                <RowAction tone="success" label="Aceitar" busy={busy === "assign"} disabled={!!busy} onClick={handleAssign} square>
                  <Check className="h-3 w-3" strokeWidth={3} />
                </RowAction>
                <RowAction tone="lavender" label="Transferir" onClick={handleTransfer} disabled={!!busy} square>
                  <ArrowLeftRight className="h-2.5 w-2.5" />
                </RowAction>
                <RowAction tone="dangerSolid" label="Finalizar" busy={busy === "close"} disabled={!!busy} onClick={handleClose} square>
                  <X className="h-2.5 w-2.5" strokeWidth={3} />
                </RowAction>
              </>
            ) : (
              <>
                {/* Atendendo: transferir / finalizar / devolver — alinhados em linha */}
                <RowAction tone="lavender" label="Transferir" onClick={handleTransfer} disabled={!!busy} square>
                  <ArrowLeftRight className="h-2.5 w-2.5" />
                </RowAction>
                <RowAction tone="dangerSolid" label="Finalizar" busy={busy === "close"} disabled={!!busy} onClick={handleClose} square>
                  <X className="h-2.5 w-2.5" strokeWidth={3} />
                </RowAction>
                <RowAction tone="neutral" label="Devolver para fila" busy={busy === "requeue"} disabled={!!busy} onClick={handleRequeue} square>
                  <RotateCcw className="h-2.5 w-2.5" />
                </RowAction>
              </>
            )}
          </div>
        )}
      </div>
    </li>
  );
};

// RowAction renders a compact action button matching the reference cards:
//  - success / dangerSolid / dark: filled, white icon (Aguardando "Aceitar", Atendendo "Finalizar")
//  - dangerOutline: light bg + colored border/icon (Aguardando "X")
//  - lavender: soft indigo chip (Atendendo "Transferir")
//  - neutral: subtle muted chip (Devolver)
// `square` switches from circular pill (Aguardando) to rounded-square (Atendendo).
// Stops propagation so clicking it doesn't also open the conversation.
type Tone = "success" | "dangerSolid" | "dangerOutline" | "dark" | "lavender" | "neutral";
const TONE_CLASSES: Record<Tone, string> = {
  success:
    "bg-emerald-500 text-white border-transparent shadow-sm hover:bg-emerald-600 active:bg-emerald-700",
  dangerSolid:
    "bg-rose-500 text-white border-transparent shadow-sm hover:bg-rose-600 active:bg-rose-700",
  dangerOutline:
    "bg-rose-500/10 text-rose-600 border-rose-500/40 hover:bg-rose-500/20 dark:text-rose-400",
  dark:
    "bg-slate-800 text-white border-transparent hover:bg-slate-700 dark:bg-slate-200 dark:text-slate-900 dark:hover:bg-white",
  lavender:
    "bg-indigo-500/15 text-indigo-600 border-indigo-500/30 hover:bg-indigo-500/25 dark:text-indigo-300",
  neutral:
    "bg-muted text-foreground border-border hover:bg-muted/80",
};
const RowAction = ({
  tone,
  label,
  onClick,
  children,
  disabled,
  busy,
  square,
}: {
  tone: Tone;
  label: string;
  onClick: (e: React.MouseEvent) => void;
  children: React.ReactNode;
  disabled?: boolean;
  busy?: boolean;
  square?: boolean;
}) => {
  const shape = square ? "rounded-md" : "rounded-full";
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      aria-busy={busy || undefined}
      disabled={disabled}
      onClick={onClick}
      className={`inline-flex h-6 w-6 items-center justify-center border transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${shape} ${TONE_CLASSES[tone]}`}
    >
      {busy ? (
        <span className="h-3 w-3 animate-spin rounded-full border-2 border-current border-t-transparent" />
      ) : (
        children
      )}
    </button>
  );
};