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
import { AppShell } from "@/components/layout/AppShell";
import { fetchReport, type ReportSummary } from "@/services/reports";
import { fetchCallHistory, type CallHistoryRow } from "@/services/callsHistory";
import { listChats, listMessages } from "@/services/chats";
import type { ChatSummary } from "@/types/chat";
import { useSessions, ensureSessionsWired } from "@/stores/sessions";


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
  const sessions = useSessions((s) => s.sessions);
  const [range, setRange] = useState<string>("30d");
  const [sessionId, setSessionId] = useState<string>(ALL_SESSIONS);
  const [report, setReport] = useState<ReportSummary | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    ensureSessionsWired();
  }, []);

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
        summary = buildSummaryFromCalls(from, to, sid, ch.rows, ch.kpis);
      } catch {
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
  }, [range, sessionId, sessions]);



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
      { name: "Atendidas", value: calls.answered },
      { name: "Perdidas", value: calls.missed },
      { name: "Outras", value: Math.max(0, calls.total - calls.answered - calls.missed) },
    ].filter((d) => d.value > 0);
  }, [calls]);

  const ticketsDonut = useMemo(() => {
    if (!tickets) return [];
    return [
      { name: "Em aberto", value: tickets.open },
      { name: "Aguardando", value: tickets.waiting },
      { name: "Finalizados", value: tickets.closed },
    ].filter((d) => d.value > 0);
  }, [tickets]);


  const topAgents = useMemo(
    () =>
      [...(report?.agents ?? [])]
        .sort((a, b) => b.closed - a.closed)
        .slice(0, 8)
        .map((a) => ({ name: (a.email || a.userId).split("@")[0], closed: a.closed })),
    [report],
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
              Relatórios
            </h2>
            <p className="text-sm text-muted-foreground">
              Chamadas e atendimentos no período selecionado
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Select value={sessionId} onValueChange={setSessionId}>
              <SelectTrigger className="h-9 w-[180px]">
                <SelectValue placeholder="Conexão" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL_SESSIONS}>Todas as conexões</SelectItem>
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
                <SelectItem value="7d">Últimos 7 dias</SelectItem>
                <SelectItem value="30d">Últimos 30 dias</SelectItem>
                <SelectItem value="90d">Últimos 90 dias</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>

        {/* KPI ROW - Chamadas */}
        <section className="space-y-3">
          <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-2">
            <Phone className="h-3.5 w-3.5" /> Chamadas
          </h3>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
            <KpiCard label="Total" value={String(calls?.total ?? 0)} icon={Phone} tone="bg-sky-500/15 text-sky-400" />
            <KpiCard label="Realizadas" value={String(calls?.outbound ?? 0)} icon={PhoneOutgoing} tone="bg-emerald-500/15 text-emerald-400" />
            <KpiCard label="Recebidas" value={String(calls?.inbound ?? 0)} hint={`${missedPct}% perdidas`} icon={PhoneIncoming} tone="bg-primary/15 text-primary" />
            <KpiCard label="Atendidas" value={String(calls?.answered ?? 0)} hint={`${answeredPct}% do total`} icon={TrendingUp} tone="bg-emerald-500/15 text-emerald-400" />
            <KpiCard label="Perdidas" value={String(calls?.missed ?? 0)} icon={Calendar} tone="bg-rose-500/15 text-rose-400" />
            <KpiCard label="Duração média" value={formatDuration(calls?.avgDurationMs ?? 0)} hint="por ligação" icon={Clock} tone="bg-violet-500/15 text-violet-400" />
          </div>
        </section>

        {/* Charts row 1 - Calls timeline + donut */}
        <div className="grid gap-4 lg:grid-cols-3">
          <ChartCard
            title="Ligações por dia"
            subtitle="Saídas vs. entradas"
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
                <Area type="monotone" dataKey="callsOut" name="Saídas" stroke={C.emerald} strokeWidth={2} fill="url(#gOut)" />
                <Area type="monotone" dataKey="callsIn" name="Entradas" stroke={C.blue} strokeWidth={2} fill="url(#gIn)" />
              </AreaChart>
            </ResponsiveContainer>
          </ChartCard>

          <ChartCard title="Status das chamadas" subtitle="Distribuição no período" icon={Phone}>
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
            <MessageSquare className="h-3.5 w-3.5" /> Atendimentos no chat
          </h3>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
            <KpiCard label="Mensagens" value={String(messages?.total ?? 0)} icon={MessageSquare} tone="bg-sky-500/15 text-sky-400" />
            <KpiCard label="Recebidas" value={String(messages?.inbound ?? 0)} icon={PhoneIncoming} tone="bg-primary/15 text-primary" />
            <KpiCard label="Enviadas" value={String(messages?.outbound ?? 0)} icon={PhoneOutgoing} tone="bg-emerald-500/15 text-emerald-400" />
            <KpiCard label="Em aberto" value={String(tickets?.open ?? 0)} icon={UsersIcon} tone="bg-amber-500/15 text-amber-400" />
            <KpiCard label="Aguardando" value={String(tickets?.waiting ?? 0)} icon={Clock} tone="bg-violet-500/15 text-violet-400" />
            <KpiCard label="Finalizados" value={String(tickets?.closed ?? 0)} icon={TrendingUp} tone="bg-emerald-500/15 text-emerald-400" />
          </div>
        </section>

        {/* Charts row 2 - Messages stacked + tickets donut */}
        <div className="grid gap-4 lg:grid-cols-3">
          <ChartCard
            title="Mensagens por dia"
            subtitle="Recebidas e enviadas"
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
                <Bar dataKey="messagesIn" name="Recebidas" stackId="m" fill={C.sky} radius={[0, 0, 0, 0]} />
                <Bar dataKey="messagesOut" name="Enviadas" stackId="m" fill={C.emerald} radius={[4, 4, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          </ChartCard>

          <ChartCard title="Tickets por status" subtitle="Snapshot atual" icon={UsersIcon}>
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


        {loading && (
          <p className="text-center text-xs text-muted-foreground">Carregando...</p>
        )}
      </div>
    </AppShell>
  );
}
