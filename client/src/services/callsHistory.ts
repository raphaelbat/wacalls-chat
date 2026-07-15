import { apiGet } from "@/lib/api";
import type { RecordingRef } from "@/types/history";

export type CallHistoryRow = {
  id: string;
  sessionId: string;
  sessionName?: string;
  direction: "inbound" | "outbound" | string;
  peer: string;
  name?: string;
  phone?: string;
  avatarUrl?: string;
  startedAt: number;
  endedAt: number;
  durationMs: number;
  endReason?: string;
  video: boolean;
  answered: boolean;
  recording?: RecordingRef | null;
};

export type CallHistoryKpis = {
  total: number;
  outbound: number;
  inbound: number;
  answered: number;
  missed: number;
  video: number;
  totalDurationMs: number;
  avgDurationMs: number;
};

export type CallHistoryResponse = {
  from: number;
  to: number;
  rows: CallHistoryRow[];
  kpis: CallHistoryKpis;
};

export const fetchCallHistory = (params: {
  from?: number;
  to?: number;
  sessionId?: string;
  direction?: "inbound" | "outbound" | "";
  status?: "answered" | "missed" | "";
  q?: string;
  limit?: number;
}) => {
  const qs = new URLSearchParams();
  if (params.from) qs.set("from", String(params.from));
  if (params.to) qs.set("to", String(params.to));
  if (params.sessionId) qs.set("sessionId", params.sessionId);
  if (params.direction) qs.set("direction", params.direction);
  if (params.status) qs.set("status", params.status);
  if (params.q) qs.set("q", params.q);
  if (params.limit) qs.set("limit", String(params.limit));
  const s = qs.toString();
  return apiGet<CallHistoryResponse>(`/api/calls${s ? `?${s}` : ""}`);
};