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