import { useEffect, useState } from "react";
import { Loader2, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  getBusinessHours,
  saveBusinessHours,
  type BusinessHoursConfig,
} from "@/services/businessHours";

const WEEKDAYS = ["Domingo", "Segunda", "Terça", "Quarta", "Quinta", "Sexta", "Sábado"];

const TIMEZONES = [
  "America/Sao_Paulo",
  "America/Manaus",
  "America/Fortaleza",
  "America/Bahia",
  "America/Cuiaba",
  "UTC",
];

interface Props {
  scope: "session" | "queue";
  scopeId: string;
  /** Texto herdado (ex.: mensagem fora de expediente da conexão). */
  fallbackMessage?: string;
}

export const BusinessHoursEditor = ({ scope, scopeId, fallbackMessage }: Props) => {
  const [cfg, setCfg] = useState<BusinessHoursConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [holiday, setHoliday] = useState("");

  useEffect(() => {
    let alive = true;
    setLoading(true);
    getBusinessHours(scope, scopeId)
      .then((c) => {
        if (!alive) return;
        setCfg({ ...c, message: c.message || fallbackMessage || "" });
      })
      .catch(() => alive && setCfg(null))
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scope, scopeId]);

  if (loading) {
    return (
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="h-3.5 w-3.5 animate-spin" /> Carregando horários...
      </div>
    );
  }
  if (!cfg) return <p className="text-xs text-muted-foreground">Não foi possível carregar os horários.</p>;

  const patchDay = (weekday: number, patch: Partial<BusinessHoursConfig["days"][number]>) =>
    setCfg({
      ...cfg,
      days: cfg.days.map((d) => (d.weekday === weekday ? { ...d, ...patch } : d)),
    });

  const save = async () => {
    setSaving(true);
    try {
      const saved = await saveBusinessHours({ ...cfg, scope, scopeId });
      setCfg(saved);
      toast.success("Horário de atendimento salvo");
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-4 rounded-lg border bg-card p-3">
      <div className="flex items-center justify-between gap-3">
        <div>
          <div className="text-sm font-medium">Horário de atendimento</div>
          <div className="text-xs text-muted-foreground">
            {scope === "queue"
              ? "Quando desativado, a fila herda o horário da conexão."
              : "Fora do horário, o cliente recebe a mensagem automática uma vez por período."}
          </div>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={cfg.enabled}
          onClick={() => setCfg({ ...cfg, enabled: !cfg.enabled })}
          className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition ${cfg.enabled ? "bg-primary" : "bg-muted"}`}
        >
          <span
            className={`inline-block h-5 w-5 transform rounded-full bg-background shadow transition ${cfg.enabled ? "translate-x-5" : "translate-x-0.5"}`}
          />
        </button>
      </div>

      {cfg.enabled && (
        <>
          <div className="space-y-1.5">
            <Label>Fuso horário</Label>
            <select
              value={cfg.timezone}
              onChange={(e) => setCfg({ ...cfg, timezone: e.target.value })}
              className="h-9 w-full rounded-md border bg-background px-2 text-sm"
            >
              {(TIMEZONES.includes(cfg.timezone) ? TIMEZONES : [cfg.timezone, ...TIMEZONES]).map((tz) => (
                <option key={tz} value={tz}>
                  {tz}
                </option>
              ))}
            </select>
          </div>

          <div className="space-y-1.5">
            <Label>Grade semanal</Label>
            <div className="space-y-1">
              {cfg.days
                .slice()
                .sort((a, b) => a.weekday - b.weekday)
                .map((d) => (
                  <div key={d.weekday} className="flex items-center gap-2">
                    <label className="flex w-28 items-center gap-2 text-xs">
                      <input
                        type="checkbox"
                        checked={d.enabled}
                        onChange={(e) => patchDay(d.weekday, { enabled: e.target.checked })}
                      />
                      {WEEKDAYS[d.weekday]}
                    </label>
                    <Input
                      type="time"
                      value={d.open}
                      disabled={!d.enabled}
                      onChange={(e) => patchDay(d.weekday, { open: e.target.value })}
                      className="h-8 w-28"
                    />
                    <span className="text-xs text-muted-foreground">até</span>
                    <Input
                      type="time"
                      value={d.close}
                      disabled={!d.enabled}
                      onChange={(e) => patchDay(d.weekday, { close: e.target.value })}
                      className="h-8 w-28"
                    />
                  </div>
                ))}
            </div>
          </div>

          <div className="space-y-1.5">
            <Label>Feriados / exceções</Label>
            <div className="flex items-center gap-2">
              <Input type="date" value={holiday} onChange={(e) => setHoliday(e.target.value)} className="h-8 w-44" />
              <Button
                type="button"
                size="sm"
                variant="secondary"
                onClick={() => {
                  if (!holiday || cfg.holidays.includes(holiday)) return;
                  setCfg({ ...cfg, holidays: [...cfg.holidays, holiday].sort() });
                  setHoliday("");
                }}
              >
                <Plus className="mr-1 h-3.5 w-3.5" /> Adicionar
              </Button>
            </div>
            {cfg.holidays.length > 0 && (
              <ul className="flex flex-wrap gap-1.5">
                {cfg.holidays.map((h) => (
                  <li key={h} className="flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs">
                    {h}
                    <button
                      type="button"
                      onClick={() => setCfg({ ...cfg, holidays: cfg.holidays.filter((x) => x !== h) })}
                      className="text-muted-foreground hover:text-destructive"
                    >
                      <Trash2 className="h-3 w-3" />
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>

          <div className="space-y-1.5">
            <Label>Mensagem fora de expediente</Label>
            <Textarea
              rows={3}
              value={cfg.message}
              onChange={(e) => setCfg({ ...cfg, message: e.target.value })}
              placeholder="Olá! Nosso atendimento está fora do horário. Retornaremos assim que abrirmos."
            />
          </div>
        </>
      )}

      <div className="flex justify-end">
        <Button size="sm" onClick={save} disabled={saving}>
          {saving && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          Salvar horários
        </Button>
      </div>
    </div>
  );
};
