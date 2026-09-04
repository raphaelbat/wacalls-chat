import { useEffect, useMemo, useState } from "react";
import {
  BarChart3,
  Calendar,
  Clock,
  MessageSquare,
  Phone,
  PhoneIncoming,
  PhoneOutgoing,
  TrendingUp,
  Users as UsersIcon,
  Mic,
  Play,
  Download,
} from "lucide-react";
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Legend,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { useTranslation } from "react-i18next";
import { SlaDashboard } from "@/components/domain/reports/SlaDashboard";
import { AppShell } from "@/components/layout/AppShell";
import { fetchReport, type ReportSummary } from "@/services/reports";
import { fetchCallHistory, type CallHistoryRow } from "@/services/callsHistory";
import { signCallRecording } from "@/services/calls";
import { isCallRecordingEnabled, setCallRecordingEnabled } from "@/lib/call-recording-pref";
import { Switch } from "@/components/ui/switch";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { toast } from "sonner";
import { listChats, listMessages } from "@/services/chats";
import type { ChatSummary } from "@/types/chat";
import { useSessions, ensureSessionsWired } from "@/stores/sessions";
import { eventStream } from "@/lib/event-stream";


import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

const RANGES: Record<string, number> = { "7d": 7, "30d": 30, "90d": 90 };
const ALL_SESSIONS = "__all__";

// Power BI-ish palette (works in dark mode)
const C = {
  blue: "#3b82f6",
  emerald: "#10b981",
  amber: "#f59e0b",
  rose: "#f43f5e",
  violet: "#8b5cf6",
  sky: "#06b6d4",
  slate: "#94a3b8",
};
const DONUT_COLORS = [C.emerald, C.rose, C.amber, C.violet, C.sky, C.blue];

const formatDuration = (ms: number) => {
  if (!ms || ms < 1000) return "0s";
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  const r = s % 60;
  return r ? `${m}m ${r}s` : `${m}m`;
};

const shortDay = (iso: string) => {
  // "2026-07-05" → "05/07"
  const [, m, d] = iso.split("-");
  return d && m ? `${d}/${m}` : iso;
};

const KpiCard = ({
  label,
  value,
  hint,
  delta,
  icon: Icon,
  tone,
}: {
  label: string;
  value: string;
  hint?: string;
  delta?: { value: number; positive?: boolean };
  icon: typeof Phone;
  tone: string;
}) => (
  <div className="rounded-xl border bg-card p-4 transition hover:shadow-md">
    <div className="flex items-start justify-between">
      <div className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
        {label}
      </div>
      <span className={`grid h-8 w-8 place-items-center rounded-lg ${tone}`}>
        <Icon className="h-4 w-4" />
      </span>
    </div>
    <div className="mt-3 text-2xl font-semibold tabular-nums">{value}</div>
    <div className="mt-1 flex items-center gap-2">
      {hint ? <span className="text-[11px] text-muted-foreground">{hint}</span> : null}
      {delta ? (
        <span
          className={`text-[11px] font-medium ${
            delta.positive ? "text-emerald-500" : "text-rose-500"
          }`}
        >
          {delta.positive ? "▲" : "▼"} {delta.value}%
        </span>
      ) : null}
    </div>
  </div>
);

const ChartCard = ({
  title,
  subtitle,
  icon: Icon,
  children,
  className = "",
}: {
  title: string;
  subtitle?: string;
  icon?: typeof BarChart3;
  children: React.ReactNode;
  className?: string;
}) => (
  <div className={`rounded-xl border bg-card p-4 ${className}`}>
    <div className="mb-3 flex items-start justify-between">
      <div>
        <div className="flex items-center gap-2 text-sm font-semibold">
          {Icon ? <Icon className="h-4 w-4 text-primary" /> : null}
          {title}
        </div>
        {subtitle ? (
          <div className="mt-0.5 text-[11px] text-muted-foreground">{subtitle}</div>
        ) : null}
      </div>
    </div>
    <div className="h-64 w-full">{children}</div>
  </div>
);

const tooltipStyle = {
  backgroundColor: "hsl(var(--card))",
  border: "1px solid hsl(var(--border))",
  borderRadius: 8,
  fontSize: 12,
  padding: "8px 10px",
};

const emptyReport = (from: number, to: number, sessionId?: string): ReportSummary => ({
  from,
  to,
  sessionId,
  messages: { total: 0, inbound: 0, outbound: 0 },
  calls: {
    total: 0, inbound: 0, outbound: 0, answered: 0, missed: 0, video: 0,
    totalDurationMs: 0, avgDurationMs: 0,
  },
  tickets: { closed: 0, waiting: 0, open: 0 },
  daily: [],
  closureReasons: [],
  agents: [],
  ratings: { total: 0, good: 0, bad: 0, awful: 0, average: 0 },
});

const buildSummaryFromCalls = (
  from: number,
  to: number,
  sessionId: string | undefined,
  rows: CallHistoryRow[],
  kpis: {
    total: number; inbound: number; outbound: number; answered: number;
    missed: number; video: number; totalDurationMs: number; avgDurationMs: number;
  },
): ReportSummary => {
  const base = emptyReport(from, to, sessionId);
  base.calls = { ...kpis };

  // Build daily buckets from `from` to `to`.
  const dayKey = (ts: number) => {
    const d = new Date(ts);
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, "0");
    const day = String(d.getDate()).padStart(2, "0");
    return `${y}-${m}-${day}`;
  };
  const buckets = new Map<string, {
    day: string; messagesIn: number; messagesOut: number;
    callsIn: number; callsOut: number; callsAnswered: number;
    callsMissed: number; ticketsClosed: number;
  }>();
  const start = new Date(from); start.setHours(0, 0, 0, 0);
  const end = new Date(to); end.setHours(0, 0, 0, 0);
  for (let d = new Date(start); d <= end; d.setDate(d.getDate() + 1)) {
    const k = dayKey(d.getTime());
    buckets.set(k, {
      day: k, messagesIn: 0, messagesOut: 0,
      callsIn: 0, callsOut: 0, callsAnswered: 0, callsMissed: 0, ticketsClosed: 0,
    });
  }
  for (const r of rows) {
    const k = dayKey(r.startedAt);
    const b = buckets.get(k);
    if (!b) continue;
    if (r.direction === "inbound") b.callsIn += 1;
    else b.callsOut += 1;
    if (r.answered) b.callsAnswered += 1;
    else if (r.direction === "inbound") b.callsMissed += 1;
  }
  base.daily = Array.from(buckets.values()).sort((a, b) => a.day.localeCompare(b.day));
  return base;
};

const mergeChatsIntoSummary = (
  summary: ReportSummary,
  chats: ChatSummary[],
): ReportSummary => {
  const from = summary.from;
  const to = summary.to;
  const dayKey = (ts: number) => {
    const d = new Date(ts);
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, "0");
    const day = String(d.getDate()).padStart(2, "0");
    return `${y}-${m}-${day}`;
  };
  const daily = new Map(summary.daily.map((d) => [d.day, { ...d }]));

  let msgsTotal = 0;
  const tickets = { closed: 0, waiting: 0, open: 0 };

  for (const c of chats) {
    if (c.isGroup) continue;
    // Only account chats whose latest activity falls in the window.
    if (c.lastTs && (c.lastTs < from || c.lastTs > to)) continue;

    msgsTotal += c.count ?? 0;
    if (c.status === "closed") tickets.closed += 1;
    else if (c.status === "waiting") tickets.waiting += 1;
    else if (c.status === "open") tickets.open += 1;

    if (c.status === "closed" && c.lastTs) {
      const k = dayKey(c.lastTs);
      const b = daily.get(k);
      if (b) b.ticketsClosed += 1;
    }
  }

  return {
    ...summary,
    messages: { total: msgsTotal, inbound: 0, outbound: 0 },
    tickets,
    daily: Array.from(daily.values()).sort((a, b) => a.day.localeCompare(b.day)),
  };
};

const mergeReportSources = (remote: ReportSummary | null, local: ReportSummary): ReportSummary => {
  if (!remote) return local;
  const hasMessageBreakdown = (r: ReportSummary) => (r.messages?.inbound ?? 0) + (r.messages?.outbound ?? 0) > 0;
  const callScore = (r: ReportSummary) => r.calls?.total ?? 0;
  const ticketScore = (r: ReportSummary) => (r.tickets?.open ?? 0) + (r.tickets?.waiting ?? 0) + (r.tickets?.closed ?? 0);
  const dailyByDay = new Map(local.daily.map((d) => [d.day, { ...d }]));

  for (const rd of remote.daily ?? []) {
    const ld = dailyByDay.get(rd.day);
    dailyByDay.set(rd.day, {
      day: rd.day,
      messagesIn: rd.messagesIn || ld?.messagesIn || 0,
      messagesOut: rd.messagesOut || ld?.messagesOut || 0,
      callsIn: rd.callsIn || ld?.callsIn || 0,
      callsOut: rd.callsOut || ld?.callsOut || 0,
      callsAnswered: rd.callsAnswered || ld?.callsAnswered || 0,
      callsMissed: rd.callsMissed || ld?.callsMissed || 0,
      ticketsClosed: rd.ticketsClosed || ld?.ticketsClosed || 0,
    });
  }

  return {
    ...local,
    ...remote,
    messages: hasMessageBreakdown(remote) ? remote.messages : local.messages,
    calls: callScore(remote) >= callScore(local) ? remote.calls : local.calls,
    tickets: ticketScore(remote) >= ticketScore(local) ? remote.tickets : local.tickets,
    daily: Array.from(dailyByDay.values()).sort((a, b) => a.day.localeCompare(b.day)),
    closureReasons: remote.closureReasons?.length ? remote.closureReasons : local.closureReasons,
    agents: remote.agents?.length ? remote.agents : local.agents,
  };
};





export default function ReportsPage() {
  const { t } = useTranslation();
  const sessions = useSessions((s) => s.sessions);
  const [range, setRange] = useState<string>("30d");
  const [sessionId, setSessionId] = useState<string>(ALL_SESSIONS);
  const [report, setReport] = useState<ReportSummary | null>(null);
  const [loading, setLoading] = useState(false);
  const [callRows, setCallRows] = useState<CallHistoryRow[]>([]);
  const [recordEnabled, setRecordEnabled] = useState(isCallRecordingEnabled());
  const [callsRefresh, setCallsRefresh] = useState(0);

  useEffect(() => {
    ensureSessionsWired();
  }, []);

  useEffect(() => eventStream.on((event) => {
    if (event.type === "call-ended") setCallsRefresh((value) => value + 1);
  }), []);

  useEffect(() => {
    const days = RANGES[range] ?? 30;
    const to = Date.now();
    const from = to - days * 24 * 60 * 60 * 1000;
    const sid = sessionId === ALL_SESSIONS ? undefined : sessionId;
    setLoading(true);

    const targetSessions = sid ? [sid] : sessions.map((s) => s.id);

    const buildFallback = async (): Promise<ReportSummary> => {
      let summary = emptyReport(from, to, sid);
      try {
        const ch = await fetchCallHistory({ from, to, sessionId: sid, limit: 5000 });
        setCallRows(ch.rows ?? []);
        summary = buildSummaryFromCalls(from, to, sid, ch.rows, ch.kpis);
      } catch {
        setCallRows([]);
        /* keep empty calls */
      }
      // Merge chat/ticket data across the selected sessions.
      try {
        const chatsBySession = await Promise.all(
          targetSessions.map(async (id) => ({ id, chats: await listChats(id).catch(() => [] as ChatSummary[]) })),
        );
        const allChats = chatsBySession.flatMap((item) => item.chats);
        summary = mergeChatsIntoSummary(summary, allChats);
        const messageTotals = { total: 0, inbound: 0, outbound: 0 };
        const dailyByDay = new Map(summary.daily.map((d) => [d.day, { ...d }]));
        await Promise.all(
          chatsBySession.flatMap(({ id, chats }) =>
            chats
              .filter((chat) => !chat.isGroup)
              .map(async (chat) => {
                const rows = await listMessages(id, chat.chatJid, { limit: 500 }).catch(() => []);
                for (const msg of rows) {
                  if (msg.ts < from || msg.ts > to) continue;
                  messageTotals.total += 1;
                  const day = new Date(msg.ts).toISOString().slice(0, 10);
                  const bucket = dailyByDay.get(day);
                  if (msg.fromMe) {
                    messageTotals.outbound += 1;
                    if (bucket) bucket.messagesOut += 1;
                  } else {
                    messageTotals.inbound += 1;
                    if (bucket) bucket.messagesIn += 1;
                  }
                }
              }),
          ),
        );
        if (messageTotals.total > 0) {
          summary = {
            ...summary,
            messages: messageTotals,
            daily: Array.from(dailyByDay.values()).sort((a, b) => a.day.localeCompare(b.day)),
          };
        }
      } catch {
        /* ignore chat aggregation errors */
      }
      return summary;
    };

    // Sempre construir localmente para garantir dados de chamadas e atendimentos,
    // já que o endpoint /api/reports pode retornar dados incompletos dependendo
    // do backend/configuração. Tentamos o backend em paralelo e usamos o que
    // tiver mais dados (calls.total + messages.total + tickets totais).
    (async () => {
      try {
        const [remote, local] = await Promise.all([
          fetchReport({ from, to, sessionId: sid }).catch(() => null),
          buildFallback(),
        ]);
        setReport(mergeReportSources(remote, local));
      } finally {
        setLoading(false);
      }
    })();
  }, [range, sessionId, sessions, callsRefresh]);



  const answeredPct = useMemo(
    () => (report?.calls.total ? Math.round((report.calls.answered / report.calls.total) * 100) : 0),
    [report],
  );
  const missedPct = useMemo(
    () => (report?.calls.inbound ? Math.round((report.calls.missed / report.calls.inbound) * 100) : 0),
    [report],
  );

  const calls = report?.calls;
  const messages = report?.messages;
  const tickets = report?.tickets;

  // Prepare chart data
  const daily = useMemo(
    () =>
      (report?.daily ?? []).map((d) => ({
        ...d,
        label: shortDay(d.day),
        callsTotal: d.callsIn + d.callsOut,
        msgsTotal: d.messagesIn + d.messagesOut,
      })),
    [report],
  );

  const callsDonut = useMemo(() => {
    if (!calls) return [];
    return [
      { name: t("pages.reports.answered", { defaultValue: "Atendidas" }), value: calls.answered },
      { name: t("pages.reports.missed", { defaultValue: "Perdidas" }), value: calls.missed },
      { name: t("pages.reports.other", { defaultValue: "Outras" }), value: Math.max(0, calls.total - calls.answered - calls.missed) },
    ].filter((d) => d.value > 0);
  }, [calls, t]);

  const ticketsDonut = useMemo(() => {
    if (!tickets) return [];
    return [
      { name: t("pages.reports.ticketsOpen", { defaultValue: "Em aberto" }), value: tickets.open },
      { name: t("pages.reports.ticketsWaiting", { defaultValue: "Aguardando" }), value: tickets.waiting },
      { name: t("pages.reports.ticketsClosed", { defaultValue: "Finalizados" }), value: tickets.closed },
    ].filter((d) => d.value > 0);
  }, [tickets, t]);


  const topAgents = useMemo(
    () =>
      [...(report?.agents ?? [])]
        .sort((a, b) => b.closed - a.closed)
        .slice(0, 8)
        .map((a) => ({ name: (a.email || a.userId).split("@")[0], closed: a.closed })),
    [report],
  );

  const recordings = useMemo(
    () => callRows.filter((r) => !!r.recording).sort((a, b) => b.startedAt - a.startedAt),
    [callRows],
  );

  const closureReasons = useMemo(
    () =>
      (report?.closureReasons ?? [])
        .slice()
        .sort((a, b) => b.count - a.count)
        .slice(0, 6),
    [report],
  );

  return (
    <AppShell>
      <div className="space-y-6 pb-12">
        {/* Header */}
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold tracking-tight flex items-center gap-2">
              <BarChart3 className="h-5 w-5 text-primary" />
              {t("pages.reports.title", { defaultValue: "Relatórios" })}
            </h2>
            <p className="text-sm text-muted-foreground">
              {t("pages.reports.subtitle", { defaultValue: "Chamadas e atendimentos no período selecionado" })}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Select value={sessionId} onValueChange={setSessionId}>
              <SelectTrigger className="h-9 w-[180px]">
                <SelectValue placeholder={t("pages.reports.connectionPlaceholder", { defaultValue: "Conexão" })} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL_SESSIONS}>{t("pages.reports.allConnections", { defaultValue: "Todas as conexões" })}</SelectItem>
                {sessions.map((s) => (
                  <SelectItem key={s.id} value={s.id}>{s.name || s.id}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={range} onValueChange={setRange}>
              <SelectTrigger className="h-9 w-[140px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="7d">{t("pages.reports.last7Days", { defaultValue: "Últimos 7 dias" })}</SelectItem>
                <SelectItem value="30d">{t("pages.reports.last30Days", { defaultValue: "Últimos 30 dias" })}</SelectItem>
                <SelectItem value="90d">{t("pages.reports.last90Days", { defaultValue: "Últimos 90 dias" })}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>

        {/* KPI ROW - Chamadas */}
        <section className="space-y-3">
          <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-2">
            <Phone className="h-3.5 w-3.5" /> {t("pages.reports.calls", { defaultValue: "Chamadas" })}
          </h3>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
            <KpiCard label={t("pages.reports.total", { defaultValue: "Total" })} value={String(calls?.total ?? 0)} icon={Phone} tone="bg-sky-500/15 text-sky-400" />
            <KpiCard label={t("pages.reports.made", { defaultValue: "Realizadas" })} value={String(calls?.outbound ?? 0)} icon={PhoneOutgoing} tone="bg-emerald-500/15 text-emerald-400" />
            <KpiCard label={t("pages.reports.received", { defaultValue: "Recebidas" })} value={String(calls?.inbound ?? 0)} hint={t("pages.reports.missedPctHint", { defaultValue: "{{pct}}% perdidas", pct: missedPct })} icon={PhoneIncoming} tone="bg-primary/15 text-primary" />
            <KpiCard label={t("pages.reports.answered", { defaultValue: "Atendidas" })} value={String(calls?.answered ?? 0)} hint={t("pages.reports.answeredPctHint", { defaultValue: "{{pct}}% do total", pct: answeredPct })} icon={TrendingUp} tone="bg-emerald-500/15 text-emerald-400" />
            <KpiCard label={t("pages.reports.missed", { defaultValue: "Perdidas" })} value={String(calls?.missed ?? 0)} icon={Calendar} tone="bg-rose-500/15 text-rose-400" />
            <KpiCard label={t("pages.reports.avgDuration", { defaultValue: "Duração média" })} value={formatDuration(calls?.avgDurationMs ?? 0)} hint={t("pages.reports.perCall", { defaultValue: "por ligação" })} icon={Clock} tone="bg-violet-500/15 text-violet-400" />
          </div>
        </section>

        {/* Charts row 1 - Calls timeline + donut */}
        <div className="grid gap-4 lg:grid-cols-3">
          <ChartCard
            title={t("pages.reports.callsPerDay", { defaultValue: "Ligações por dia" })}
            subtitle={t("pages.reports.outVsIn", { defaultValue: "Saídas vs. entradas" })}
            icon={TrendingUp}
            className="lg:col-span-2"
          >
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={daily} margin={{ top: 10, right: 12, left: -10, bottom: 0 }}>
                <defs>
                  <linearGradient id="gOut" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor={C.emerald} stopOpacity={0.5} />
                    <stop offset="100%" stopColor={C.emerald} stopOpacity={0} />
                  </linearGradient>
                  <linearGradient id="gIn" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor={C.blue} stopOpacity={0.5} />
                    <stop offset="100%" stopColor={C.blue} stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid stroke="hsl(var(--border))" strokeDasharray="3 3" vertical={false} />
                <XAxis dataKey="label" tick={{ fontSize: 11, fill: "hsl(var(--muted-foreground))" }} tickLine={false} axisLine={false} />
                <YAxis tick={{ fontSize: 11, fill: "hsl(var(--muted-foreground))" }} tickLine={false} axisLine={false} width={30} />
                <Tooltip contentStyle={tooltipStyle} labelStyle={{ color: "hsl(var(--foreground))" }} />
                <Legend wrapperStyle={{ fontSize: 12 }} />
                <Area type="monotone" dataKey="callsOut" name={t("pages.reports.outbound", { defaultValue: "Saídas" })} stroke={C.emerald} strokeWidth={2} fill="url(#gOut)" />
                <Area type="monotone" dataKey="callsIn" name={t("pages.reports.inbound", { defaultValue: "Entradas" })} stroke={C.blue} strokeWidth={2} fill="url(#gIn)" />
              </AreaChart>
            </ResponsiveContainer>
          </ChartCard>

          <ChartCard title={t("pages.reports.callStatus", { defaultValue: "Status das chamadas" })} subtitle={t("pages.reports.periodDistribution", { defaultValue: "Distribuição no período" })} icon={Phone}>
            <ResponsiveContainer width="100%" height="100%">
              <PieChart>
                <Tooltip contentStyle={tooltipStyle} />
                <Pie data={callsDonut} dataKey="value" nameKey="name" innerRadius={55} outerRadius={85} paddingAngle={2}>
                  {callsDonut.map((_, i) => (
                    <Cell key={i} fill={DONUT_COLORS[i % DONUT_COLORS.length]} />
                  ))}
                </Pie>
                <Legend wrapperStyle={{ fontSize: 12 }} verticalAlign="bottom" iconType="circle" />
              </PieChart>
            </ResponsiveContainer>
          </ChartCard>
        </div>

        {/* KPI ROW - Atendimentos */}
        <section className="space-y-3">
          <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-2">
            <MessageSquare className="h-3.5 w-3.5" /> {t("pages.reports.chatService", { defaultValue: "Atendimentos no chat" })}
          </h3>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
            <KpiCard label={t("pages.reports.messages", { defaultValue: "Mensagens" })} value={String(messages?.total ?? 0)} icon={MessageSquare} tone="bg-sky-500/15 text-sky-400" />
            <KpiCard label={t("pages.reports.received", { defaultValue: "Recebidas" })} value={String(messages?.inbound ?? 0)} icon={PhoneIncoming} tone="bg-primary/15 text-primary" />
            <KpiCard label={t("pages.reports.sent", { defaultValue: "Enviadas" })} value={String(messages?.outbound ?? 0)} icon={PhoneOutgoing} tone="bg-emerald-500/15 text-emerald-400" />
            <KpiCard label={t("pages.reports.ticketsOpen", { defaultValue: "Em aberto" })} value={String(tickets?.open ?? 0)} icon={UsersIcon} tone="bg-amber-500/15 text-amber-400" />
            <KpiCard label={t("pages.reports.ticketsWaiting", { defaultValue: "Aguardando" })} value={String(tickets?.waiting ?? 0)} icon={Clock} tone="bg-violet-500/15 text-violet-400" />
            <KpiCard label={t("pages.reports.ticketsClosed", { defaultValue: "Finalizados" })} value={String(tickets?.closed ?? 0)} icon={TrendingUp} tone="bg-emerald-500/15 text-emerald-400" />
          </div>
        </section>

        {/* Charts row 2 - Messages stacked + tickets donut */}
        <div className="grid gap-4 lg:grid-cols-3">
          <ChartCard
            title={t("pages.reports.messagesPerDay", { defaultValue: "Mensagens por dia" })}
            subtitle={t("pages.reports.receivedAndSent", { defaultValue: "Recebidas e enviadas" })}
            icon={MessageSquare}
            className="lg:col-span-2"
          >
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={daily} margin={{ top: 10, right: 12, left: -10, bottom: 0 }}>
                <CartesianGrid stroke="hsl(var(--border))" strokeDasharray="3 3" vertical={false} />
                <XAxis dataKey="label" tick={{ fontSize: 11, fill: "hsl(var(--muted-foreground))" }} tickLine={false} axisLine={false} />
                <YAxis tick={{ fontSize: 11, fill: "hsl(var(--muted-foreground))" }} tickLine={false} axisLine={false} width={30} />
                <Tooltip contentStyle={tooltipStyle} cursor={{ fill: "hsl(var(--muted))", opacity: 0.3 }} />
                <Legend wrapperStyle={{ fontSize: 12 }} />
                <Bar dataKey="messagesIn" name={t("pages.reports.received", { defaultValue: "Recebidas" })} stackId="m" fill={C.sky} radius={[0, 0, 0, 0]} />
                <Bar dataKey="messagesOut" name={t("pages.reports.sent", { defaultValue: "Enviadas" })} stackId="m" fill={C.emerald} radius={[4, 4, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          </ChartCard>

          <ChartCard title={t("pages.reports.ticketsByStatus", { defaultValue: "Tickets por status" })} subtitle={t("pages.reports.currentSnapshot", { defaultValue: "Snapshot atual" })} icon={UsersIcon}>
            <ResponsiveContainer width="100%" height="100%">
              <PieChart>
                <Tooltip contentStyle={tooltipStyle} />
                <Pie data={ticketsDonut} dataKey="value" nameKey="name" innerRadius={55} outerRadius={85} paddingAngle={2}>
                  {ticketsDonut.map((_, i) => (
                    <Cell key={i} fill={[C.amber, C.violet, C.emerald][i % 3]} />
                  ))}
                </Pie>
                <Legend wrapperStyle={{ fontSize: 12 }} verticalAlign="bottom" iconType="circle" />
              </PieChart>
            </ResponsiveContainer>
          </ChartCard>
        </div>


        {/* SLA por fila e conexão */}
        <SlaDashboard
          from={report?.from ?? Date.now() - (RANGES[range] ?? 30) * 86400000}
          to={report?.to ?? Date.now()}
          sessionId={sessionId === ALL_SESSIONS ? undefined : sessionId}
        />

        {/* Gravações de ligações */}
        <section className="space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-2">
              <Mic className="h-3.5 w-3.5" /> {t("pages.reports.callRecordings", { defaultValue: "Gravações de ligações" })}
            </h3>
            <div className="flex items-center gap-2 rounded-md border border-border px-3 py-1.5">
              <Switch
                id="rec-toggle"
                checked={recordEnabled}
                onCheckedChange={(v) => {
                  setRecordEnabled(v);
                  setCallRecordingEnabled(v);
                  toast.success(
                    v
                      ? t("pages.reports.recordingEnabledToast", { defaultValue: "Gravação de chamadas ativada" })
                      : t("pages.reports.recordingDisabledToast", { defaultValue: "Gravação de chamadas desativada" }),
                  );
                }}
              />
              <Label htmlFor="rec-toggle" className="text-xs font-medium">
                {t("pages.reports.saveRecordingsLabel", { defaultValue: "Salvar gravações das ligações" })}
              </Label>
            </div>
          </div>

          <div className="rounded-lg border border-border overflow-hidden">
            {recordings.length === 0 ? (
              <p className="p-6 text-center text-xs text-muted-foreground">
                {t("pages.reports.noRecordings", { defaultValue: "Nenhuma gravação no período. Ative a opção acima para gravar as próximas ligações." })}
              </p>
            ) : (
              <table className="w-full text-sm">
                <thead className="bg-muted/40 text-xs uppercase tracking-wider text-muted-foreground">
                  <tr>
                    <th className="px-3 py-2 text-left font-medium">{t("pages.reports.colContact", { defaultValue: "Contato" })}</th>
                    <th className="px-3 py-2 text-left font-medium">{t("pages.reports.colDate", { defaultValue: "Data" })}</th>
                    <th className="px-3 py-2 text-left font-medium">{t("pages.reports.colDuration", { defaultValue: "Duração" })}</th>
                    <th className="px-3 py-2 text-right font-medium">{t("pages.reports.colRecording", { defaultValue: "Gravação" })}</th>
                  </tr>
                </thead>
                <tbody>
                  {recordings.map((row) => (
                    <RecordingRow key={row.id} row={row} />
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </section>

        {loading && (
          <p className="text-center text-xs text-muted-foreground">{t("common.loading", { defaultValue: "Carregando..." })}</p>
        )}
      </div>
    </AppShell>
  );
}

function RecordingRow({ row }: { row: CallHistoryRow }) {
  const { t, i18n } = useTranslation();
  const [src, setSrc] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const sign = async (): Promise<{ shareUrl: string; downloadUrl: string } | null> => {
    try {
      setBusy(true);
      return await signCallRecording(row.sessionId, row.id, 900);
    } catch (e) {
      toast.error((e as Error).message);
      return null;
    } finally {
      setBusy(false);
    }
  };

  return (
    <tr className="border-t border-border">
      <td className="px-3 py-2">
        <span className="font-medium">{row.name || row.phone || row.peer}</span>
        <span className="ml-2 text-xs text-muted-foreground">
          {row.direction === "outbound" ? t("pages.reports.dirOutbound", { defaultValue: "Saída" }) : t("pages.reports.dirInbound", { defaultValue: "Entrada" })}
        </span>
      </td>
      <td className="px-3 py-2 text-xs text-muted-foreground">
        {new Date(row.startedAt).toLocaleString(i18n.language || "pt-BR")}
      </td>
      <td className="px-3 py-2 text-xs text-muted-foreground">{formatDuration(row.durationMs)}</td>
      <td className="px-3 py-2">
        <div className="flex items-center justify-end gap-2">
          {src ? (
            <audio src={src} controls className="h-8 max-w-[240px]" />
          ) : (
            <Button
              size="sm"
              variant="outline"
              disabled={busy}
              onClick={async () => {
                const links = await sign();
                if (links) setSrc(links.shareUrl);
              }}
            >
              <Play className="h-3.5 w-3.5 mr-1" /> {t("pages.reports.listen", { defaultValue: "Ouvir" })}
            </Button>
          )}
          <Button
            size="sm"
            variant="ghost"
            disabled={busy}
            onClick={async () => {
              const links = await sign();
              if (links) window.open(links.downloadUrl, "_blank");
            }}
          >
            <Download className="h-3.5 w-3.5" />
          </Button>
        </div>
      </td>
    </tr>
  );
}
