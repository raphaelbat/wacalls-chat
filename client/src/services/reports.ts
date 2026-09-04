import { apiGet } from "@/lib/api";

export type ReportSummary = {
  from: number;
  to: number;
  sessionId?: string;
  messages: { total: number; inbound: number; outbound: number };
  calls: {
    total: number;
    inbound: number;
    outbound: number;
    answered: number;
    missed: number;
    video: number;
    totalDurationMs: number;
    avgDurationMs: number;
  };
  tickets: { closed: number; waiting: number; open: number };
  daily: Array<{
    day: string;
    messagesIn: number;
    messagesOut: number;
    callsIn: number;
    callsOut: number;
    callsAnswered: number;
    callsMissed: number;
    ticketsClosed: number;
  }>;
  closureReasons: Array<{ label: string; count: number }>;
  agents: Array<{ userId: string; email?: string; closed: number }>;
  ratings: { total: number; good: number; bad: number; awful: number; average: number };
};

export const fetchReport = (params: { from?: number; to?: number; sessionId?: string }) => {
  const q = new URLSearchParams();
  if (params.from) q.set("from", String(params.from));
  if (params.to) q.set("to", String(params.to));
  if (params.sessionId) q.set("sessionId", params.sessionId);
  const qs = q.toString();
  return apiGet<ReportSummary>(`/api/reports/summary${qs ? `?${qs}` : ""}`);
};
export type SlaMetrics = {
  conversations: number;
  answered: number;
  resolved: number;
  pending: number;
  avgFirstResponseMs: number;
  maxFirstResponseMs: number;
  avgResolutionMs: number;
  maxResolutionMs: number;
  firstResponseBreach: number;
  resolutionBreach: number;
  compliance: number;
  ratingCount: number;
  ratingAvg: number;
};

export type SlaGroup = { id: string; label: string; metrics: SlaMetrics };

export type SlaAlert = {
  sessionId: string;
  sessionName?: string;
  chatJid: string;
  name?: string;
  queueId?: string;
  queueName?: string;
  kind: "first-response" | "resolution" | "unanswered";
  firstResponseMs?: number;
  resolutionMs?: number;
  waitingMs?: number;
  ts: number;
};

export type SlaReport = {
  from: number;
  to: number;
  sessionId?: string;
  firstResponseTargetMs: number;
  resolutionTargetMs: number;
  overall: SlaMetrics;
  byQueue: SlaGroup[];
  bySession: SlaGroup[];
  byAgent: SlaGroup[];
  alerts: SlaAlert[];
};

export const fetchSlaReport = (params: {
  from?: number;
  to?: number;
  sessionId?: string;
  firstResponseTargetMs?: number;
  resolutionTargetMs?: number;
}) => {
  const q = new URLSearchParams();
  if (params.from) q.set("from", String(params.from));
  if (params.to) q.set("to", String(params.to));
  if (params.sessionId) q.set("sessionId", params.sessionId);
  if (params.firstResponseTargetMs) q.set("firstResponseTargetMs", String(params.firstResponseTargetMs));
  if (params.resolutionTargetMs) q.set("resolutionTargetMs", String(params.resolutionTargetMs));
  const qs = q.toString();
  return apiGet<SlaReport>(`/api/reports/sla${qs ? `?${qs}` : ""}`);
};
