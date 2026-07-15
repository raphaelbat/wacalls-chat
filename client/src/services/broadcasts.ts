import { apiGet, apiPost, apiDelete } from "@/lib/api";

export type BroadcastStatus = "draft" | "running" | "paused" | "finished";
export type BroadcastRecipientStatus = "pending" | "sending" | "sent" | "failed";

export interface Broadcast {
  id: string;
  ownerId: string;
  name: string;
  sessionId: string;
  scheduleAt: number;
  minGapRecSec: number;
  maxGapRecSec: number;
  minGapSeqSec: number;
  maxGapSeqSec: number;
  retries: number;
  retryDelaySec: number;
  messages: string[];
  status: BroadcastStatus;
  createdAt: number;
  updatedAt: number;
  total: number;
  pending: number;
  sent: number;
  failed: number;
}

export interface BroadcastRecipient {
  id: string;
  broadcastId: string;
  phone: string;
  name?: string;
  vars?: Record<string, string>;
  status: BroadcastRecipientStatus;
  attempts: number;
  lastError?: string;
  updatedAt: number;
}

export interface NewBroadcastInput {
  name: string;
  sessionId: string;
  scheduleAt?: number;
  minGapRecSec: number;
  maxGapRecSec: number;
  minGapSeqSec: number;
  maxGapSeqSec: number;
  retries: number;
  retryDelaySec: number;
  messages: string[];
  recipients: { phone: string; name?: string; vars?: Record<string, string> }[];
}

export const listBroadcasts = () =>
  apiGet<{ broadcasts: Broadcast[] }>("/api/broadcasts").then((r) => r.broadcasts ?? []);

export const createBroadcast = (body: NewBroadcastInput) =>
  apiPost<Broadcast>("/api/broadcasts", body);

export const deleteBroadcast = (id: string) => apiDelete(`/api/broadcasts/${id}`);

export const startBroadcast = (id: string) =>
  apiPost<{ status: string }>(`/api/broadcasts/${id}/start`, {});

export const pauseBroadcast = (id: string) =>
  apiPost<{ status: string }>(`/api/broadcasts/${id}/pause`, {});

/**
 * Parse a free-form CSV-ish text where the first column is the phone and
 * additional columns become variables (header row optional). Returns
 * recipients ready to send to the broadcast API.
 */
export function parseRecipientsCSV(raw: string): {
  recipients: { phone: string; name?: string; vars?: Record<string, string> }[];
  headers: string[];
} {
  const lines = raw
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter((l) => l.length > 0 && !l.startsWith("—") && !l.startsWith("-"));
  if (lines.length === 0) return { recipients: [], headers: [] };

  let headers: string[] = [];
  let dataStart = 0;
  const first = lines[0].split(/[,;\t]/).map((s) => s.trim());
  const firstColIsHeader = first[0] && !/^[+\d]/.test(first[0]);
  if (firstColIsHeader) {
    headers = first.map((h) => h.toLowerCase());
    dataStart = 1;
  }

  const recipients: { phone: string; name?: string; vars?: Record<string, string> }[] = [];
  for (let i = dataStart; i < lines.length; i++) {
    const cols = lines[i].split(/[,;\t]/).map((s) => s.trim());
    const phone = cols[0]?.replace(/[^\d+]/g, "");
    if (!phone || phone.replace(/\D/g, "").length < 8) continue;
    const r: { phone: string; name?: string; vars?: Record<string, string> } = { phone };
    if (headers.length > 1) {
      const vars: Record<string, string> = {};
      for (let j = 1; j < headers.length && j < cols.length; j++) {
        if (cols[j]) vars[headers[j]] = cols[j];
      }
      if (vars.nome || vars.name) r.name = vars.nome || vars.name;
      r.vars = vars;
    } else if (cols[1]) {
      r.name = cols[1];
    }
    recipients.push(r);
  }
  return { recipients, headers };
}