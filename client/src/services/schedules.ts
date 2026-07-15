// Local-only scheduled calls store. The backend runner will be wired in a
// follow-up — for now we persist agendamentos in localStorage so the UI is
// usable end-to-end (CRUD + listing) without touching the database.

export type ScheduledCall = {
  id: string;
  // "call" agenda uma ligação no horário marcado.
  // "message" envia uma mensagem (texto + mídia opcional) pelo WhatsApp.
  kind?: "call" | "message";
  sessionId: string;
  sessionName?: string;
  phone: string;
  name?: string;
  scheduledAt: number;
  note?: string;
  // "pending"  = aguardando a data/hora marcada (NÃO dispara automaticamente).
  // "armed"    = pronto para disparar quando chegar a data/hora (auto-start).
  // "done"     = concluído.
  // "cancelled"= pausado/cancelado pelo usuário.
  status: "pending" | "armed" | "done" | "cancelled";
  createdAt: number;
  // ---- Áudio da ligação agendada ----
  // "none" | "record" (áudio gravado pelo usuário) | "tts" (síntese).
  audioMode?: "none" | "record" | "tts";
  // Data URL do áudio gravado (curto, < 1 MB).
  audioDataUrl?: string;
  audioMime?: string;
  // Provedor de TTS quando audioMode === "tts".
  ttsProvider?: "piper" | "elevenlabs";
  ttsText?: string;
  // ---- Mensagem agendada ----
  messageText?: string;
  mediaDataUrl?: string;
  mediaName?: string;
  mediaMime?: string;
};

const KEY = "primevoip.scheduledCalls.v1";

const read = (): ScheduledCall[] => {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return [];
    const v = JSON.parse(raw);
    return Array.isArray(v) ? (v as ScheduledCall[]) : [];
  } catch {
    return [];
  }
};

const write = (rows: ScheduledCall[]) => {
  try {
    localStorage.setItem(KEY, JSON.stringify(rows));
  } catch {
    /* noop */
  }
};

export const listSchedules = (): ScheduledCall[] =>
  read().sort((a, b) => a.scheduledAt - b.scheduledAt);

export const createSchedule = (s: Omit<ScheduledCall, "id" | "createdAt" | "status">): ScheduledCall => {
  const row: ScheduledCall = {
    ...s,
    id: `sch_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 7)}`,
    createdAt: Date.now(),
    status: "pending",
  };
  write([row, ...read()]);
  return row;
};

export const updateSchedule = (id: string, patch: Partial<ScheduledCall>) => {
  write(read().map((r) => (r.id === id ? { ...r, ...patch } : r)));
};

export const deleteSchedule = (id: string) => {
  write(read().filter((r) => r.id !== id));
};