import { useCallback, useEffect, useState } from "react";
import { Info, Loader2, Lock, RefreshCw, ShieldAlert, X } from "lucide-react";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import type { SessionInfo } from "@/types/session";
import { getInstancePlan } from "@/lib/instance-plan";
import { fetchAccountHealth, type AccountHealth } from "@/services/sessions";

// Renders the WhatsApp account limits / health modal. Data is derived from
// what the frontend already knows about the session — there is no backend
// health endpoint yet, so capping/timelock fields fall back to neutral
// defaults. Clicking "Atualizar agora" simply re-mounts via the parent.
export function AccountHealthDialog({
  open,
  onOpenChange,
  session,
  onRefresh,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  session: SessionInfo;
  onRefresh?: () => void;
}) {
  const _plan = getInstancePlan(session.id);
  void _plan;

  const [health, setHealth] = useState<AccountHealth | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const h = await fetchAccountHealth(session.id);
      setHealth(h);
    } catch (e) {
      setError((e as Error)?.message ?? "Falha ao consultar WhatsApp");
    } finally {
      setLoading(false);
    }
  }, [session.id]);

  useEffect(() => {
    if (open) void load();
  }, [open, load]);

  const isRestricted = health?.restricted ?? (session.state === "logged_out" && session.paired);
  const fmtMs = (ms?: number) =>
    !ms
      ? "—"
      : new Date(ms).toLocaleString("pt-BR", {
      day: "2-digit",
      month: "2-digit",
      year: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  const queriedAt = health?.queriedAt ?? Date.now();

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent showCloseButton={false} className="max-w-md gap-0 p-0">
        <div className="flex items-center justify-between border-b px-5 py-4">
          <DialogTitle className="flex items-center gap-2 text-base font-semibold">
            <Info className="h-4 w-4 text-muted-foreground" />
            {session.name || session.id}
          </DialogTitle>
          <button
            type="button"
            onClick={() => onOpenChange(false)}
            className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
            aria-label="Fechar"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="space-y-4 px-5 py-5">
          {error ? (
            <div className="rounded-lg border border-rose-500/40 bg-rose-500/5 p-4 text-xs text-rose-400">
              {error}
            </div>
          ) : null}
          {isRestricted ? (
            <div className="rounded-lg border border-rose-500/40 bg-rose-500/5 p-4">
              <div className="flex items-center gap-2 text-sm font-semibold text-rose-400">
                <Lock className="h-4 w-4" />
                Restringido — todos os companions
              </div>
              <p className="mt-1.5 text-xs leading-relaxed text-muted-foreground">
                WhatsApp bloqueou esta conta de iniciar novas conversas via API.
                Aguarde o fim do timelock ou esta conta foi marcada por
                comportamento suspeito.
              </p>
              <span className="mt-3 inline-flex items-center gap-1 rounded-full bg-rose-500/10 px-2 py-0.5 text-[11px] font-semibold text-rose-400">
                <Lock className="h-3 w-3" /> Expira hoje
              </span>
            </div>
          ) : (
            <div className="rounded-lg border border-emerald-500/30 bg-emerald-500/5 p-4">
              <div className="flex items-center gap-2 text-sm font-semibold text-emerald-400">
                <ShieldAlert className="h-4 w-4" />
                Conta saudável
              </div>
              <p className="mt-1.5 text-xs leading-relaxed text-muted-foreground">
                Nenhuma restrição ativa detectada. A conta pode iniciar novas
                conversas normalmente.
              </p>
            </div>
          )}

          <Section title="CONEXÃO" icon={<Info className="h-3.5 w-3.5" />}>
            <Field
              label="Status"
              value={
                health?.connected ? (
                  <span className="text-emerald-400">CONECTADO</span>
                ) : (
                  <span className="text-rose-400">OFFLINE</span>
                )
              }
            />
            <Field label="Login" mono value={health?.loggedIn ? "SIM" : "NÃO"} />
            <Field label="Número (JID)" mono value={health?.jid || "—"} />
            <Field label="LID" mono value={health?.lid || "—"} />
            <Field label="Push name" value={health?.pushName || "—"} />
            <Field
              label="Tipo"
              value={health?.isBusiness ? "Business" : "Pessoal"}
            />
            <Field label="Plataforma" mono value={health?.platform || "—"} />
            <Field label="Estado" mono value={health?.state || session.state} />
          </Section>


          <p className="text-[11px] text-muted-foreground">
            🕐 Última consulta ao WhatsApp: {fmtMs(queriedAt)}
          </p>
        </div>

        <div className="flex items-center justify-end gap-2 border-t bg-muted/30 px-5 py-3">
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
            Fechar
          </Button>
          <Button
            size="sm"
            disabled={loading}
            className="bg-primary text-primary-foreground hover:bg-primary"
            onClick={() => {
              void load();
              onRefresh?.();
            }}
          >
            {loading ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <RefreshCw className="h-3.5 w-3.5" />
            )}{" "}
            Atualizar agora
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

const Section = ({
  title,
  icon,
  children,
}: {
  title: string;
  icon: React.ReactNode;
  children: React.ReactNode;
}) => (
  <div className="rounded-lg border bg-card/50 p-4">
    <div className="mb-3 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
      {icon}
      {title}
    </div>
    <div className="grid grid-cols-2 gap-x-4 gap-y-3">{children}</div>
  </div>
);

const Field = ({
  label,
  value,
  mono,
}: {
  label: string;
  value: React.ReactNode;
  mono?: boolean;
}) => (
  <div className="min-w-0">
    <div className="text-[11px] text-muted-foreground">{label}</div>
    <div className={`mt-0.5 truncate text-xs ${mono ? "font-mono" : "font-medium"}`}>
      {value}
    </div>
  </div>
);