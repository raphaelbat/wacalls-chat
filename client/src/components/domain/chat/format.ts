import type { ChatMessage } from "@/types/chat";
import { formatPhone } from "@/lib/phone-format";

// isGroupJid returns true for any JID that should be rendered under the
// "Grupos" tab — classic groups (@g.us), WhatsApp Channels/Newsletters
// (@newsletter) and Community broadcast lists (@broadcast).
export const isGroupJid = (jid: string | undefined | null): boolean => {
  if (!jid) return false;
  return (
    jid.endsWith("@g.us") ||
    jid.endsWith("@newsletter") ||
    jid.endsWith("@broadcast")
  );
};

const KIND_LABEL: Record<ChatMessage["kind"], string> = {
  text: "",
  image: "📷 Imagem",
  video: "🎬 Vídeo",
  audio: "🎤 Áudio",
  document: "📄 Documento",
  sticker: "🟦 Figurinha",
  location: "📍 Localização",
  contact: "👤 Contato",
  reaction: "🙂 Reação",
  note: "📝 Nota",
  unknown: "Mensagem",
};

export const previewBody = (kind: ChatMessage["kind"], body: string): string => {
  if (kind === "text") return body;
  const label = KIND_LABEL[kind] ?? "";
  if (!body) return label || "Mensagem";
  return `${label} — ${body}`;
};

export const formatPeer = (jid: string): string => {
  const local = jid.split("@")[0] ?? jid;
  if (jid.endsWith("@g.us")) return `Grupo ${local.slice(0, 8)}`;
  if (jid.endsWith("@newsletter")) return `Canal ${local.slice(0, 8)}`;
  if (jid.endsWith("@broadcast")) return `Lista ${local.slice(0, 8)}`;
  // LIDs (@lid) are WhatsApp internal hidden-user IDs, NOT phone numbers
  // (they can be 15 digits and would otherwise be mis-rendered as
  // "+199347350294740"). Show a neutral placeholder so callers know the real
  // PN must be resolved server-side via /api/sessions/{sid}/lid-pn/{jid}.
  if (jid.endsWith("@lid")) return "Número oculto";
  // Delegate personal-number formatting to the shared phone formatter so the
  // exact same string ("+55 (81) 9999-9999") appears in lists, modals,
  // notifications, call cards and the chat header.
  const digits = local.split(":")[0].split(".")[0];
  if (/^\d{10,15}$/.test(digits)) return formatPhone(digits);
  return local;
};

export const formatTime = (ts: number): string => {
  if (!ts) return "";
  const d = new Date(ts);
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
};

export const formatDayHeader = (ts: number): string => {
  if (!ts) return "";
  const d = new Date(ts);
  const today = new Date();
  const yest = new Date();
  yest.setDate(today.getDate() - 1);
  const sameDay = (a: Date, b: Date) =>
    a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
  if (sameDay(d, today)) return "Hoje";
  if (sameDay(d, yest)) return "Ontem";
  return d.toLocaleDateString();
};

// formatRelative renders "agora", "há X min", "há X h", "há X d" — used by
// the ticket list so agents can see how long the customer has been waiting.
export const formatRelative = (ts: number): string => {
  if (!ts) return "";
  const diff = Math.max(0, Date.now() - ts);
  const sec = Math.floor(diff / 1000);
  if (sec < 45) return "agora";
  const min = Math.floor(sec / 60);
  if (min < 60) return `há ${min} min`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `há ${hr} h`;
  const day = Math.floor(hr / 24);
  if (day < 7) return `há ${day} d`;
  const wk = Math.floor(day / 7);
  if (wk < 5) return `há ${wk} sem`;
  return new Date(ts).toLocaleDateString();
};