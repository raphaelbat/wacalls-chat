import { create } from "zustand";
import { eventStream, type BrokerEvent } from "@/lib/event-stream";
import { listChats, listMessages, markChatRead as markChatReadAPI } from "@/services/chats";
import type { ChatMessage, ChatSummary } from "@/types/chat";

type State = {
  chatsBySession: Record<string, ChatSummary[]>;
  messagesBySession: Record<string, Record<string, ChatMessage[]>>;
  loadingChats: Record<string, boolean>;
  loadingMessages: Record<string, boolean>;
  activeJidBySession: Record<string, string | null>;
};

// --- Persistência local --------------------------------------------------
// A lista de conversas (incluindo avatarUrl, nome, último preview) é
// cacheada em localStorage para que a aba de atendimento abra
// instantaneamente com os dados da sessão anterior, enquanto o backend
// atualiza em segundo plano. Antes, cada visita ficava em branco até o
// fetchChats responder, e os <img> de avatar baixavam de novo.
const CACHE_KEY = "voipinho.chats.cache.v1";
const MAX_CACHED_PER_SESSION = 200;

const loadCache = (): Record<string, ChatSummary[]> => {
  if (typeof window === "undefined") return {};
  try {
    const raw = window.localStorage.getItem(CACHE_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as Record<string, ChatSummary[]>;
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
};

let persistTimer: ReturnType<typeof setTimeout> | null = null;
const schedulePersist = (chatsBySession: Record<string, ChatSummary[]>) => {
  if (typeof window === "undefined") return;
  if (persistTimer) clearTimeout(persistTimer);
  persistTimer = setTimeout(() => {
    try {
      const compact: Record<string, ChatSummary[]> = {};
      for (const [sid, list] of Object.entries(chatsBySession)) {
        // Apenas o suficiente para a lista lateral. Mensagens não entram aqui.
        compact[sid] = list.slice(0, MAX_CACHED_PER_SESSION);
      }
      window.localStorage.setItem(CACHE_KEY, JSON.stringify(compact));
    } catch {
      /* quota / privacy — ignora */
    }
  }, 300);
};

export const useChats = create<State>(() => ({
  chatsBySession: loadCache(),
  messagesBySession: {},
  loadingChats: {},
  loadingMessages: {},
  activeJidBySession: {},
}));

export const resetChatsStore = (): void => {
  if (persistTimer) clearTimeout(persistTimer);
  persistTimer = null;
  if (typeof window !== "undefined") {
    try {
      window.localStorage.removeItem(CACHE_KEY);
    } catch {
      /* ignore */
    }
  }
  useChats.setState({
    chatsBySession: {},
    messagesBySession: {},
    loadingChats: {},
    loadingMessages: {},
    activeJidBySession: {},
  });
};

// Persiste sempre que a coleção de chats mudar.
useChats.subscribe((state, prev) => {
  if (state.chatsBySession !== prev.chatsBySession) {
    schedulePersist(state.chatsBySession);
  }
});

export const setActiveChat = (sessionId: string, jid: string | null) =>
  useChats.setState((s) => ({ activeJidBySession: { ...s.activeJidBySession, [sessionId]: jid } }));

// Optimistically updates the local conversation status so the chat moves
// between the "Atendendo" / "Aguardando" tabs immediately after the user
// presses Atender/Finalizar, without waiting for the server SSE round-trip.
export const setChatStatus = (
  sessionId: string,
  jid: string,
  status: "open" | "waiting" | "closed",
  assignedUserId?: string | null,
) => {
  useChats.setState((s) => {
    const list = s.chatsBySession[sessionId];
    if (!list) return {} as Partial<State>;
    const idx = list.findIndex((c) => c.chatJid === jid);
    if (idx < 0) return {} as Partial<State>;
    const next = [...list];
    next[idx] = {
      ...next[idx],
      status,
      assignedUserId: assignedUserId === undefined ? next[idx].assignedUserId : assignedUserId ?? undefined,
    };
    return { chatsBySession: { ...s.chatsBySession, [sessionId]: next } };
  });
};

export const fetchChats = async (sessionId: string) => {
  // Se já temos cache local, refresca de forma silenciosa para não
  // esconder a lista atrás do "Carregando conversas…".
  const hasCache = (useChats.getState().chatsBySession[sessionId]?.length ?? 0) > 0;
  if (!hasCache) {
    useChats.setState((s) => ({ loadingChats: { ...s.loadingChats, [sessionId]: true } }));
  }
  try {
    const chats = (await listChats(sessionId)) ?? [];
    // Faz merge para não perder avatarUrl/name vindos do cache quando o
    // backend ainda não resolveu — evita "piscar" o placeholder de letra.
    useChats.setState((s) => {
      const prev = s.chatsBySession[sessionId] ?? [];
      const prevByJid = new Map(prev.map((c) => [c.chatJid, c]));
      const merged = chats.map((c) => {
        const old = prevByJid.get(c.chatJid);
        if (!old) return c;
        return {
          ...c,
          avatarUrl: c.avatarUrl || old.avatarUrl,
          name: c.name || old.name,
        };
      });
      return { chatsBySession: { ...s.chatsBySession, [sessionId]: merged } };
    });
    // Faz pré-carregamento das fotos para que apareçam imediatamente
    // quando o usuário rolar a lista — sem o delay de "imagem aparecendo
    // só depois de um tempo" relatado pelo usuário.
    if (typeof window !== "undefined") {
      for (const c of chats) {
        if (c.avatarUrl) {
          const img = new Image();
          img.decoding = "async";
          img.src = c.avatarUrl;
        }
      }
    }
    // Auto-retry quando a primeira chamada chega vazia logo após o login
    // / troca de empresa: nesse momento o whatsmeow ainda pode não ter
    // sincronizado as mensagens da sessão. Antes o usuário precisava
    // apertar F5 para ver os atendimentos aparecerem.
    if (chats.length === 0 && !hasCache) {
      scheduleEmptyRetry(sessionId);
    } else {
      clearEmptyRetry(sessionId);
    }
  } catch (err) {
    // eslint-disable-next-line no-console
    console.error("[chats] fetchChats failed", sessionId, err);
    useChats.setState((s) => ({ chatsBySession: { ...s.chatsBySession, [sessionId]: s.chatsBySession[sessionId] ?? [] } }));
  } finally {
    useChats.setState((s) => ({ loadingChats: { ...s.loadingChats, [sessionId]: false } }));
  }
};

// Retries silenciosos para a aba de atendimento abrir cheia mesmo
// quando a primeira resposta veio vazia (sessão recém-pareada, troca
// de empresa, primeiro acesso do dia). Tenta em 1s, 3s e 6s; cancela
// assim que receber dados ou trocar de sessão.
const emptyRetries: Record<string, ReturnType<typeof setTimeout>[]> = {};
const clearEmptyRetry = (sessionId: string) => {
  const list = emptyRetries[sessionId];
  if (!list) return;
  for (const t of list) clearTimeout(t);
  delete emptyRetries[sessionId];
};
const scheduleEmptyRetry = (sessionId: string) => {
  clearEmptyRetry(sessionId);
  emptyRetries[sessionId] = [1000, 3000, 6000].map((ms) =>
    setTimeout(() => {
      const cur = useChats.getState().chatsBySession[sessionId];
      if (cur && cur.length > 0) return;
      void fetchChats(sessionId);
    }, ms),
  );
};

export const fetchMessages = async (sessionId: string, jid: string) => {
  const key = `${sessionId}::${jid}`;
  useChats.setState((s) => ({ loadingMessages: { ...s.loadingMessages, [key]: true } }));
  try {
    const messages = ((await listMessages(sessionId, jid, { limit: 100 })) ?? []).map(normalizeDeletedMessage);
    useChats.setState((s) => {
      const perSession = { ...(s.messagesBySession[sessionId] ?? {}) };
      perSession[jid] = messages;
      return { messagesBySession: { ...s.messagesBySession, [sessionId]: perSession } };
    });
  } catch (err) {
    // eslint-disable-next-line no-console
    console.error("[chats] fetchMessages failed", sessionId, jid, err);
  } finally {
    useChats.setState((s) => ({ loadingMessages: { ...s.loadingMessages, [key]: false } }));
  }
};

// Local persistence of message bodies/media so we can still display the
// original content after a refresh if the contact (or operator) revokes a
// message. The backend wipes the body on revoke, so without this snapshot
// the operator would see "(conteúdo não disponível)" after reloading.
const SNAPSHOT_KEY = "chat:msg:snapshot:v1";
const SNAPSHOT_MAX = 800;
type Snapshot = { body?: string; kind?: ChatMessage["kind"]; mediaUrl?: string; mediaMime?: string; fileName?: string };
const loadSnapshots = (): Record<string, Snapshot> => {
  try {
    const raw = localStorage.getItem(SNAPSHOT_KEY);
    return raw ? (JSON.parse(raw) as Record<string, Snapshot>) : {};
  } catch {
    return {};
  }
};
const saveSnapshot = (id: string, snap: Snapshot) => {
  try {
    const map = loadSnapshots();
    map[id] = snap;
    const keys = Object.keys(map);
    if (keys.length > SNAPSHOT_MAX) {
      for (const k of keys.slice(0, keys.length - SNAPSHOT_MAX)) delete map[k];
    }
    localStorage.setItem(SNAPSHOT_KEY, JSON.stringify(map));
  } catch {
    /* ignore quota */
  }
};
const getSnapshot = (id: string): Snapshot | undefined => loadSnapshots()[id];

// Some WhatsApp/whatsmeow flows emit revoked messages without setting the
// deleted flag — the body simply becomes a localized placeholder. Detect
// those so the bubble still renders with the "Mensagem apagada" badge and
// strikethrough instead of showing the placeholder as a normal message.
const DELETED_PLACEHOLDERS = new Set([
  "mensagem",
  "esta mensagem foi apagada",
  "voce apagou esta mensagem",
  "você apagou esta mensagem",
  "this message was deleted",
  "you deleted this message",
]);
const looksDeletedPlaceholder = (body: string, kind: ChatMessage["kind"]): boolean => {
  if (kind !== "text") return false;
  const norm = (body || "").trim().toLowerCase();
  if (!norm) return false;
  return DELETED_PLACEHOLDERS.has(norm);
};

const normalizeDeletedMessage = (msg: ChatMessage): ChatMessage => {
  const placeholder = !msg.deleted && looksDeletedPlaceholder(msg.body, msg.kind);
  if (!msg.deleted && !placeholder) {
    if (msg.id && (msg.body || msg.mediaUrl)) {
      saveSnapshot(msg.id, {
        body: msg.body,
        kind: msg.kind,
        mediaUrl: msg.mediaUrl,
        mediaMime: msg.mediaMime,
        fileName: msg.fileName,
      });
    }
    return msg;
  }
  const snap = msg.id ? getSnapshot(msg.id) : undefined;
  return {
    ...msg,
    deleted: true,
    originalBody: msg.originalBody ?? snap?.body,
    originalKind: msg.originalKind ?? snap?.kind,
    originalMediaUrl: msg.originalMediaUrl ?? snap?.mediaUrl,
    originalMediaMime: msg.originalMediaMime ?? snap?.mediaMime,
    originalFileName: msg.originalFileName ?? snap?.fileName,
  };
};

const upsertMessage = (msg: ChatMessage) => {
  const placeholder = !msg.deleted && looksDeletedPlaceholder(msg.body, msg.kind);
  const effectivelyDeleted = msg.deleted || placeholder;
  // Persist non-deleted snapshots so we can recover them after refresh.
  // Skip when the incoming body itself is the deletion placeholder — we don't
  // want to overwrite a real snapshot with "Mensagem".
  if (msg.id && !effectivelyDeleted && (msg.body || msg.mediaUrl)) {
    saveSnapshot(msg.id, {
      body: msg.body,
      kind: msg.kind,
      mediaUrl: msg.mediaUrl,
      mediaMime: msg.mediaMime,
      fileName: msg.fileName,
    });
  }
  useChats.setState((s) => {
    // Update message list for the open conversation.
    const perSession = { ...(s.messagesBySession[msg.sessionId] ?? {}) };
    const existing = perSession[msg.chatJid] ?? [];
    const existingIdx = existing.findIndex((m) => m.id === msg.id);
    if (existingIdx >= 0) {
      // Edit / delete updates arrive as a re-emit of the same id.
      const next = [...existing];
      const prevMsg = next[existingIdx];
      const merged: ChatMessage = { ...prevMsg, ...msg };
      // When a delete (revoke) update arrives, preserve the previous content
      // locally so the operator can still read what the contact had sent.
      if (effectivelyDeleted && !prevMsg.deleted) {
        const snap = getSnapshot(msg.id);
        merged.deleted = true;
        merged.originalBody = prevMsg.originalBody ?? prevMsg.body ?? snap?.body;
        merged.originalKind = prevMsg.originalKind ?? prevMsg.kind ?? snap?.kind;
        merged.originalMediaUrl = prevMsg.originalMediaUrl ?? prevMsg.mediaUrl ?? snap?.mediaUrl;
        merged.originalMediaMime = prevMsg.originalMediaMime ?? prevMsg.mediaMime ?? snap?.mediaMime;
        merged.originalFileName = prevMsg.originalFileName ?? prevMsg.fileName ?? snap?.fileName;
      }
      next[existingIdx] = merged;
      perSession[msg.chatJid] = next;
    } else {
      // First time we see this id. If it arrives already-deleted (e.g. on
      // history load after a refresh), try to restore the original snapshot
      // captured from a previous session.
      let toInsert = msg;
      if (effectivelyDeleted) {
        const snap = getSnapshot(msg.id);
        toInsert = {
          ...msg,
          deleted: true,
          originalBody: msg.originalBody ?? snap?.body,
          originalKind: msg.originalKind ?? snap?.kind,
          originalMediaUrl: msg.originalMediaUrl ?? snap?.mediaUrl,
          originalMediaMime: msg.originalMediaMime ?? snap?.mediaMime,
          originalFileName: msg.originalFileName ?? snap?.fileName,
        };
      }
      perSession[msg.chatJid] = [...existing, toInsert].sort((a, b) => a.ts - b.ts);
    }


    // Update chat summary (move/insert at top).
    const chats = [...(s.chatsBySession[msg.sessionId] ?? [])];
    const idx = chats.findIndex((c) => c.chatJid === msg.chatJid);
    const prev = idx >= 0 ? chats[idx] : undefined;
    const isActive = s.activeJidBySession[msg.sessionId] === msg.chatJid;
    let unread = prev?.unread ?? 0;
    if (!msg.fromMe && !isActive) unread += 1;
    if (msg.fromMe || isActive) unread = 0;
    const summary: ChatSummary = {
      ...(prev ?? {}),
      chatJid: msg.chatJid,
      lastMessage: msg.body,
      lastKind: msg.kind,
      lastTs: msg.ts,
      lastFromMe: msg.fromMe,
      count: prev ? prev.count + 1 : 1,
      name: prev?.name,
      isGroup: prev?.isGroup,
      status: prev?.status ?? (msg.fromMe ? "open" : "waiting"),
      assignedUserId: prev?.assignedUserId,
      unread,
      lastReadTs: prev?.lastReadTs,
      avatarUrl: prev?.avatarUrl,
    };
    if (idx >= 0) chats.splice(idx, 1);
    chats.unshift(summary);
    return {
      messagesBySession: { ...s.messagesBySession, [msg.sessionId]: perSession },
      chatsBySession: { ...s.chatsBySession, [msg.sessionId]: chats },
    };
  });
  // If the message landed in the active chat, sync read state with backend.
  const state = useChats.getState();
  if (state.activeJidBySession[msg.sessionId] === msg.chatJid && !msg.fromMe) {
    void markChatReadAPI(msg.sessionId, msg.chatJid, msg.ts).catch(() => {});
  }
};

let wired = false;
export const ensureChatsWired = (): void => {
  if (wired) return;
  wired = true;
  eventStream.on((ev: BrokerEvent) => {
    if (ev.type === "message") upsertMessage(ev.message);
    if (ev.type === "chat-meta") upsertMeta(ev.meta);
  });
};

const upsertMeta = (meta: import("@/types/chat").ChatMeta) => {
  useChats.setState((s) => {
    const list = [...(s.chatsBySession[meta.sessionId] ?? [])];
    const idx = list.findIndex((c) => c.chatJid === meta.chatJid);
    if (idx >= 0) {
      list[idx] = {
        ...list[idx],
        name: meta.name || list[idx].name,
        isGroup: meta.isGroup,
        status: meta.status,
        assignedUserId: meta.assignedUserId,
        lastReadTs: meta.lastReadTs ?? list[idx].lastReadTs,
        avatarUrl: meta.avatarUrl || list[idx].avatarUrl,
        unread:
          meta.lastReadTs && list[idx].lastTs && meta.lastReadTs >= list[idx].lastTs
            ? 0
            : list[idx].unread,
      };
    } else {
      list.unshift({
        chatJid: meta.chatJid,
        lastMessage: "",
        lastKind: "text",
        lastTs: meta.updatedAt,
        lastFromMe: false,
        count: 0,
        name: meta.name,
        isGroup: meta.isGroup,
        status: meta.status,
        assignedUserId: meta.assignedUserId,
        unread: 0,
        lastReadTs: meta.lastReadTs,
        avatarUrl: meta.avatarUrl,
      });
    }
    return { chatsBySession: { ...s.chatsBySession, [meta.sessionId]: list } };
  });
};

// markChatAsRead is invoked when the user opens a conversation. It resets the
// local unread badge immediately and tells the backend to persist the cursor.
export const markChatAsRead = (sessionId: string, jid: string) => {
  useChats.setState((s) => {
    const list = s.chatsBySession[sessionId];
    if (!list) return {} as Partial<State>;
    const idx = list.findIndex((c) => c.chatJid === jid);
    if (idx < 0) return {} as Partial<State>;
    if (!list[idx].unread) return {} as Partial<State>;
    const next = [...list];
    next[idx] = { ...next[idx], unread: 0, lastReadTs: next[idx].lastTs };
    return { chatsBySession: { ...s.chatsBySession, [sessionId]: next } };
  });
  void markChatReadAPI(sessionId, jid).catch(() => {});
};