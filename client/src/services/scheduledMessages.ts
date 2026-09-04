import { apiGet, apiPost, apiDelete } from "@/lib/api";

export type ScheduledMessage = {
  id: string;
  sessionId: string;
  chatJid: string;
  text: string;
  runAt: number;
  kind: "scheduled" | "followup";
  status: "pending" | "sent" | "failed" | "cancelled";
  error?: string;
  createdAt: number;
  sentAt?: number;
};

export const listScheduledMessages = (sessionId: string, chatJid?: string) =>
  apiGet<{ schedules: ScheduledMessage[] }>(
    `/api/sessions/${sessionId}/schedules${chatJid ? `?chat=${encodeURIComponent(chatJid)}` : ""}`,
  ).then((r) => r.schedules ?? []);

export const createScheduledMessage = (
  sessionId: string,
  input: { chatJid: string; text: string; runAt: number; kind?: "scheduled" | "followup" },
) => apiPost<ScheduledMessage>(`/api/sessions/${sessionId}/schedules`, input);

export const cancelScheduledMessage = (sessionId: string, id: string) =>
  apiDelete(`/api/sessions/${sessionId}/schedules/${id}`);
