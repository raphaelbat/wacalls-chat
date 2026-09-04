import { useEffect, useMemo, useState } from "react";
import { AlertTriangle, Gauge, Clock, CheckCircle2, Timer } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { fetchSlaReport, type SlaGroup, type SlaReport } from "@/services/reports";
import { toast } from "sonner";

type Props = { from: number; to: number; sessionId?: string };

const fmtDur = (ms: number) => {
  if (!ms || ms < 0) return "—";
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ${m % 60}m`;
  return `${Math.floor(h / 24)}d ${h % 24}h`;
};

const toneForCompliance = (pct: number) =>
  pct >= 90 ? "text-emerald-400" : pct >= 70 ? "text-amber-400" : "text-rose-400";

const ALERT_LABEL: Record<string, string> = {
  "unanswered": "Sem resposta",
  "first-response": "1ª resposta estourada",
  "resolution": "Resolução estourada",
};

const GroupTable = ({
  title,
  groups,
  showCsat,
}: {
  title: string;
  groups: SlaGroup[];
  showCsat?: boolean;
}) => (
  <div className="rounded-lg border border-border overflow-hidden">
    <div className="border-b border-border bg-muted/40 px-3 py-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
      {title}
    </div>
    {groups.length === 0 ? (
      <p className="p-6 text-center text-xs text-muted-foreground">Sem dados no período.</p>
    ) : (
      <table className="w-full text-sm">
        <thead className="bg-muted/20 text-[11px] uppercase tracking-wider text-muted-foreground">
          <tr>
            <th className="px-3 py-2 text-left font-medium">Nome</th>
            <th className="px-3 py-2 text-right font-medium">Atend.</th>
            <th className="px-3 py-2 text-right font-medium">1ª resposta</th>
            <th className="px-3 py-2 text-right font-medium">Resolução</th>
            <th className="px-3 py-2 text-right font-medium">Fora do SLA</th>
            {showCsat ? <th className="px-3 py-2 text-right font-medium">CSAT</th> : null}
            <th className="px-3 py-2 text-right font-medium">Cumprimento</th>
          </tr>
        </thead>
        <tbody>
          {groups.map((g) => (
            <tr key={g.id} className="border-t border-border/60">
              <td className="px-3 py-2">{g.label}</td>
              <td className="px-3 py-2 text-right">{g.metrics.conversations}</td>
              <td className="px-3 py-2 text-right">{fmtDur(g.metrics.avgFirstResponseMs)}</td>
              <td className="px-3 py-2 text-right">{fmtDur(g.metrics.avgResolutionMs)}</td>
              <td className="px-3 py-2 text-right">
                {g.metrics.firstResponseBreach + g.metrics.resolutionBreach}
              </td>
              {showCsat ? (
                <td className="px-3 py-2 text-right">
                  {g.metrics.ratingCount > 0
                    ? `${g.metrics.ratingAvg.toFixed(1)} (${g.metrics.ratingCount})`
                    : "—"}
                </td>
              ) : null}
              <td className={`px-3 py-2 text-right font-medium ${toneForCompliance(g.metrics.compliance)}`}>
                {g.metrics.compliance}%
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    )}
  </div>
);

export const SlaDashboard = ({ from, to, sessionId }: Props) => {
  const [respMin, setRespMin] = useState(5);
  const [resHours, setResHours] = useState(4);
  const [data, setData] = useState<SlaReport | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    let cancel = false;
    setLoading(true);
    fetchSlaReport({
      from,
      to,
      sessionId,
      firstResponseTargetMs: Math.max(1, respMin) * 60 * 1000,
      resolutionTargetMs: Math.max(1, resHours) * 60 * 60 * 1000,
    })
      .then((r) => !cancel && setData(r))
      .catch((error: Error) => {
        if (cancel) return;
        setData(null);
        toast.error(`Não foi possível carregar o SLA: ${error.message}`);
      })
      .finally(() => !cancel && setLoading(false));
    return () => {
      cancel = true;
    };
  }, [from, to, sessionId, respMin, resHours]);

  const kpis = useMemo(() => {
    const m = data?.overall;
    return [
      { label: "Atendimentos", value: String(m?.conversations ?? 0), icon: Gauge, tone: "bg-sky-500/15 text-sky-400" },
      { label: "1ª resposta média", value: fmtDur(m?.avgFirstResponseMs ?? 0), icon: Clock, tone: "bg-primary/15 text-primary" },
      { label: "Resolução média", value: fmtDur(m?.avgResolutionMs ?? 0), icon: Timer, tone: "bg-violet-500/15 text-violet-400" },
      { label: "Resolvidos", value: String(m?.resolved ?? 0), icon: CheckCircle2, tone: "bg-emerald-500/15 text-emerald-400" },
      {
        label: "Fora do SLA",
        value: String((m?.firstResponseBreach ?? 0) + (m?.resolutionBreach ?? 0)),
        icon: AlertTriangle,
        tone: "bg-rose-500/15 text-rose-400",
      },
      {
        label: "Cumprimento",
        value: `${m?.compliance ?? 0}%`,
        icon: Gauge,
        tone: "bg-amber-500/15 text-amber-400",
      },
    ];
  }, [data]);

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-2">
          <Gauge className="h-3.5 w-3.5" /> SLA por fila e conexão
        </h3>
        <div className="flex items-end gap-3">
          <div className="space-y-1">
            <Label className="text-[11px] text-muted-foreground">Meta 1ª resposta (min)</Label>
            <Input
              type="number"
              min={1}
              className="h-8 w-28"
              value={respMin}
              onChange={(e) => setRespMin(Math.max(1, Number(e.target.value) || 1))}
            />
          </div>
          <div className="space-y-1">
            <Label className="text-[11px] text-muted-foreground">Meta resolução (h)</Label>
            <Input
              type="number"
              min={1}
              className="h-8 w-28"
              value={resHours}
              onChange={(e) => setResHours(Math.max(1, Number(e.target.value) || 1))}
            />
          </div>
        </div>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
        {kpis.map((k) => (
          <div key={k.label} className="rounded-lg border border-border bg-card p-3">
            <div className="flex items-center gap-2">
              <span className={`grid h-7 w-7 place-items-center rounded-md ${k.tone}`}>
                <k.icon className="h-3.5 w-3.5" />
              </span>
              <span className="text-[11px] text-muted-foreground">{k.label}</span>
            </div>
            <p className="mt-2 text-lg font-semibold tracking-tight">{k.value}</p>
          </div>
        ))}
      </div>

      <div className="grid gap-3 lg:grid-cols-2">
        <GroupTable title="Por fila" groups={data?.byQueue ?? []} />
        <GroupTable title="Por conexão" groups={data?.bySession ?? []} />
        <GroupTable title="Por atendente" groups={data?.byAgent ?? []} showCsat />
      </div>

      <div className="rounded-lg border border-border overflow-hidden">
        <div className="border-b border-border bg-muted/40 px-3 py-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-2">
          <AlertTriangle className="h-3.5 w-3.5 text-rose-400" /> Alertas de SLA
        </div>
        {(data?.alerts?.length ?? 0) === 0 ? (
          <p className="p-6 text-center text-xs text-muted-foreground">
            {loading ? "Carregando..." : "Nenhum atendimento fora da meta no período."}
          </p>
        ) : (
          <table className="w-full text-sm">
            <thead className="bg-muted/20 text-[11px] uppercase tracking-wider text-muted-foreground">
              <tr>
                <th className="px-3 py-2 text-left font-medium">Contato</th>
                <th className="px-3 py-2 text-left font-medium">Fila</th>
                <th className="px-3 py-2 text-left font-medium">Conexão</th>
                <th className="px-3 py-2 text-left font-medium">Alerta</th>
                <th className="px-3 py-2 text-right font-medium">Tempo</th>
              </tr>
            </thead>
            <tbody>
              {data!.alerts.map((a) => (
                <tr key={`${a.sessionId}:${a.chatJid}:${a.kind}`} className="border-t border-border/60">
                  <td className="px-3 py-2">{a.name || a.chatJid.split("@")[0]}</td>
                  <td className="px-3 py-2 text-muted-foreground">{a.queueName || "Sem fila"}</td>
                  <td className="px-3 py-2 text-muted-foreground">{a.sessionName || a.sessionId}</td>
                  <td className="px-3 py-2">
                    <span className="rounded-full bg-rose-500/15 px-2 py-0.5 text-[11px] font-medium text-rose-400">
                      {ALERT_LABEL[a.kind] ?? a.kind}
                    </span>
                  </td>
                  <td className="px-3 py-2 text-right">
                    {fmtDur(a.waitingMs || a.resolutionMs || a.firstResponseMs || 0)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </section>
  );
};
