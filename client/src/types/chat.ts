export interface ChatMessage {
  id: string;
  sessionId: string;
  chatJid: string;
  senderJid: string;
  fromMe: boolean;
  ts: number;
  kind:
    | "text"
    | "image"
    | "video"
    | "audio"
    | "document"
    | "sticker"
    | "location"
    | "contact"
    | "reaction"
    | "note"
    | "unknown";
  body: string;
  mediaMime?: string;
  mediaUrl?: string;
  fileName?: string;
  fileSize?: number;
  quotedId?: string;
  senderName?: string;
  edited?: boolean;
  deleted?: boolean;
  // Snapshot of the message content before it was revoked by the sender.
  // Populated locally by the client so we can keep displaying what was sent.
  originalBody?: string;
  originalKind?: ChatMessage["kind"];
  originalMediaUrl?: string;
  originalMediaMime?: string;
  originalFileName?: string;
}

export interface ChatSummary {
  chatJid: string;
  lastMessage: string;
  lastKind: ChatMessage["kind"];
  lastTs: number;
  lastFromMe: boolean;
  count: number;
  name?: string;
  isGroup?: boolean;
  status?: "waiting" | "open" | "closed" | "group";
  assignedUserId?: string;
  unread?: number;
  lastReadTs?: number;
  avatarUrl?: string;
}

export interface ChatMeta {
  sessionId: string;
  chatJid: string;
  name: string;
  isGroup: boolean;
  status: "waiting" | "open" | "closed" | "group";
  assignedUserId?: string;
  updatedAt: number;
  lastReadTs?: number;
  avatarUrl?: string;
}

export interface ChatClosure {
  id: number;
  sessionId: string;
  chatJid: string;
  userId: string;
  userEmail?: string;
  reason: string;
  closedAt: number;
}

// ChatEvent is a lifecycle/audit entry for a conversation. The chat timeline
// renders these as small system pills ("Conversa criada por · 17:32",
// "Conversa aberta por Admin · 17:35", "Conversa fechada por … · 18:01").
export interface ChatEvent {
  id: number;
  sessionId: string;
  chatJid: string;
  kind:
    | "created"
    | "waiting"
    | "opened"
    | "closed"
    | "requeued"
    | "transferred"
    | "call_incoming"
    | "call_outgoing"
    | "call_answered"
    | "call_missed"
    | "call_rejected"
    | "call_canceled"
    | "call_no_answer"
    | "call_ended";
  userId?: string;
  userEmail?: string;
  detail?: string;
  ts: number;
}