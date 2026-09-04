/**
 * Campanhas de mídia.
 *
 * Uma mensagem para uma lista de contatos, disparada devagar e alternando entre
 * os números conectados. O ritmo é a parte séria da tela: os padrões são
 * conservadores de propósito, e a estimativa de duração aparece antes de
 * começar, não no meio.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowLeft,
  Loader2,
  Megaphone,
  Pause,
  Paperclip,
  Play,
  Plus,
  Trash2,
  TriangleAlert,
  Users,
} from "lucide-react";
import { toast } from "sonner";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import { AudiencePicker, type Destinatario } from "@/components/domain/campaign/AudiencePicker";
import {
  addTargets,
  clearTargets,
  createCampaign,
  deleteCampaign,
  estimativa,
  getCampaign,
  listCampaigns,
  listTargets,
  pauseCampaign,
  startCampaign,
  tipoDaMidia,
  updateCampaign,
  uploadCampaignMedia,
  type Campaign,
  type CampaignComProgresso,
  type CampaignProgress,
  type CampaignTarget,
} from "@/services/campaigns";
import { useSessions } from "@/stores/sessions";

const ROTULO_STATUS: Record<string, string> = {
  draft: "rascunho",
  running: "disparando",
  paused: "pausada",
  finished: "concluída",
};

const COR_STATUS: Record<string, string> = {
  draft: "bg-muted-foreground/40",
  running: "bg-emerald-500",
  paused: "bg-amber-500",
  finished: "bg-sky-500",
};

const DIAS = [
  { v: "1", t: "seg" },
  { v: "2", t: "ter" },
  { v: "3", t: "qua" },
  { v: "4", t: "qui" },
  { v: "5", t: "sex" },
  { v: "6", t: "sáb" },
  { v: "0", t: "dom" },
];

const Campo = ({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) => (
  <div className="space-y-1.5">
    <Label className="text-xs font-medium">{label}</Label>
    {children}
    {hint ? <p className="text-[11px] leading-snug text-muted-foreground">{hint}</p> : null}
  </div>
);

// ---------------------------------------------------------------------------

const Editor = ({ id, onVoltar }: { id: string; onVoltar: () => void }) => {
  const [c, setC] = useState<Campaign | null>(null);
  const [prog, setProg] = useState<CampaignProgress>({ total: 0, pending: 0, sent: 0, failed: 0, skipped: 0 });
  const [alvos, setAlvos] = useState<CampaignTarget[]>([]);
  const [ocupado, setOcupado] = useState(false);
  const [limpando, setLimpando] = useState(false);
  const arquivoRef = useRef<HTMLInputElement>(null);
  const sessions = useSessions((s) => s.sessions);

  const conectados = useMemo(() => sessions.filter((s) => s.state === "open"), [sessions]);

  const recarregar = useCallback(async () => {
    const r = await getCampaign(id);
    setC(r.campaign);
    setProg(r.progress);
    setAlvos(await listTargets(id).catch(() => []));
  }, [id]);

  useEffect(() => {
    void recarregar().catch((e) => toast.error((e as Error).message));
  }, [recarregar]);

  // Enquanto dispara, o progresso se atualiza sozinho — a pessoa fica olhando.
  useEffect(() => {
    if (c?.status !== "running") return;
    const t = setInterval(() => void recarregar().catch(() => {}), 5000);
    return () => clearInterval(t);
  }, [c?.status, recarregar]);

  if (!c) {
    return (
      <div className="grid place-items-center py-20">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  const rodando = c.status === "running";
  const set = (campos: Partial<Campaign>) => setC({ ...c, ...campos });

  const salvar = async (extra: Partial<Campaign> = {}) => {
    setOcupado(true);
    try {
      const salvo = await updateCampaign(c.id, { ...c, ...extra });
      setC(salvo);
      return salvo;
    } finally {
      setOcupado(false);
    }
  };

  const subirArquivo = async (f: File) => {
    setOcupado(true);
    try {
      const r = await uploadCampaignMedia(f);
      await salvar({ mediaUrl: r.url, mediaKind: tipoDaMidia(r.mime, r.filename), filename: r.filename });
      toast.success("Arquivo anexado");
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setOcupado(false);
    }
  };

  const adicionar = async (lista: Destinatario[]) => {
    setOcupado(true);
    try {
      const r = await addTargets(c.id, lista);
      setProg(r.progress);
      setAlvos(await listTargets(c.id).catch(() => []));
      const repetidos = r.recebidos - r.adicionados;
      toast.success(
        repetidos > 0
          ? `${r.adicionados} adicionados · ${repetidos} já estavam na lista`
          : `${r.adicionados} contatos adicionados`,
      );
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setOcupado(false);
    }
  };

  const disparar = async () => {
    setOcupado(true);
    try {
      await salvar();
      await startCampaign(c.id);
      await recarregar();
      toast.success("Campanha iniciada");
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setOcupado(false);
    }
  };

  const pausar = async () => {
    try {
      await pauseCampaign(c.id);
      await recarregar();
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  const tempo = estimativa(prog.pending, c, conectados.length);
  const semMensagem = !c.text.trim() && !c.mediaUrl;
  const falhas = alvos.filter((a) => a.status === "failed");

  return (
    <div className="mx-auto w-full max-w-3xl space-y-5">
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="ghost" size="sm" onClick={onVoltar}>
          <ArrowLeft className="mr-1.5 h-4 w-4" />
          Campanhas
        </Button>
        <Input
          value={c.name}
          onChange={(e) => set({ name: e.target.value })}
          onBlur={() => void salvar().catch(() => {})}
          className="h-9 w-56 text-sm font-medium"
          disabled={rodando}
        />
        <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <span className={`h-2 w-2 rounded-full ${COR_STATUS[c.status]}`} />
          {ROTULO_STATUS[c.status]}
        </span>
        <div className="ml-auto flex gap-2">
          {rodando ? (
            <Button variant="outline" size="sm" onClick={pausar}>
              <Pause className="mr-1.5 h-4 w-4" />
              Pausar
            </Button>
          ) : (
            <Button size="sm" onClick={disparar} disabled={ocupado || semMensagem || prog.pending === 0}>
              {ocupado ? <Loader2 className="mr-1.5 h-4 w-4 animate-spin" /> : <Play className="mr-1.5 h-4 w-4" />}
              {c.status === "paused" ? "Continuar" : "Disparar"}
            </Button>
          )}
        </div>
      </div>

      {c.lastError ? (
        <div className="flex items-start gap-2 rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2.5 text-xs text-amber-700 dark:text-amber-400">
          <TriangleAlert className="mt-px h-3.5 w-3.5 shrink-0" />
          <span>A campanha parou: {c.lastError}</span>
        </div>
      ) : null}

      {/* ------------------------------------------------------- progresso */}
      {prog.total > 0 ? (
        <div className="rounded-xl border bg-card p-4">
          <div className="mb-2 flex flex-wrap items-baseline justify-between gap-2">
            <span className="text-sm font-medium">
              {prog.sent} de {prog.total} enviadas
            </span>
            <span className="text-xs text-muted-foreground">
              {prog.pending} na fila
              {prog.failed ? ` · ${prog.failed} falharam` : ""}
              {tempo && !rodando ? ` · ${tempo}` : ""}
            </span>
          </div>
          <div className="h-2 overflow-hidden rounded-full bg-muted">
            <div
              className="h-full rounded-full bg-primary transition-all"
              style={{ width: `${prog.total ? (prog.sent / prog.total) * 100 : 0}%` }}
            />
          </div>
        </div>
      ) : null}

      {/* --------------------------------------------------------- mensagem */}
      <section className="space-y-3 rounded-xl border bg-card p-4">
        <h2 className="text-sm font-semibold">Mensagem</h2>
        <Campo
          label="Texto"
          hint="Use {{primeiro_nome}} ou {{nome}} para chamar cada pessoa pelo nome — mensagem personalizada é denunciada muito menos."
        >
          <Textarea
            rows={4}
            value={c.text}
            onChange={(e) => set({ text: e.target.value })}
            onBlur={() => void salvar().catch(() => {})}
            disabled={rodando}
            placeholder="Oi {{primeiro_nome}}, tudo bem?"
          />
        </Campo>

        <input
          ref={arquivoRef}
          type="file"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f) void subirArquivo(f);
          }}
        />
        {c.mediaUrl ? (
          <div className="flex items-center gap-2 rounded-lg border bg-muted/20 px-3 py-2">
            <Paperclip className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            <span className="min-w-0 flex-1 truncate text-xs">
              {c.filename || c.mediaUrl} <span className="text-muted-foreground">({c.mediaKind})</span>
            </span>
            <Button
              variant="ghost"
              size="sm"
              disabled={rodando}
              onClick={() => void salvar({ mediaUrl: "", mediaKind: "", filename: "" })}
            >
              Remover
            </Button>
          </div>
        ) : (
          <Button variant="outline" size="sm" onClick={() => arquivoRef.current?.click()} disabled={rodando || ocupado}>
            <Paperclip className="mr-1.5 h-3.5 w-3.5" />
            Anexar imagem, vídeo, áudio ou documento
          </Button>
        )}
        {c.mediaUrl ? (
          <p className="text-[11px] text-muted-foreground">Com anexo, o texto acima vai como legenda.</p>
        ) : null}
      </section>

      {/* ------------------------------------------------------ destinatários */}
      <section className="space-y-3 rounded-xl border bg-card p-4">
        <div className="flex items-center justify-between gap-2">
          <h2 className="text-sm font-semibold">Quem recebe</h2>
          {prog.total > 0 ? (
            <Button variant="ghost" size="sm" disabled={rodando} onClick={() => setLimpando(true)}>
              <Trash2 className="mr-1.5 h-3.5 w-3.5" />
              Limpar lista ({prog.total})
            </Button>
          ) : null}
        </div>
        {rodando ? (
          <p className="rounded-lg bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
            Pause a campanha para mexer na lista.
          </p>
        ) : (
          <AudiencePicker onAdicionar={adicionar} ocupado={ocupado} />
        )}
        {falhas.length ? (
          <details className="rounded-lg border">
            <summary className="cursor-pointer px-3 py-2 text-xs font-medium">
              {falhas.length} envio{falhas.length === 1 ? "" : "s"} com erro
            </summary>
            <ul className="max-h-40 space-y-1 overflow-y-auto border-t px-3 py-2 text-[11px] text-muted-foreground">
              {falhas.slice(0, 50).map((f) => (
                <li key={f.id} className="truncate">
                  <span className="font-mono">{f.jid.replace("@s.whatsapp.net", "")}</span> — {f.error}
                </li>
              ))}
            </ul>
          </details>
        ) : null}
      </section>

      {/* ------------------------------------------------------------ ritmo */}
      <section className="space-y-4 rounded-xl border bg-card p-4">
        <div>
          <h2 className="text-sm font-semibold">Ritmo do disparo</h2>
          <p className="mt-1 text-[11px] leading-snug text-muted-foreground">
            Os valores que vêm marcados são propositalmente devagar. Acelerar aumenta a chance de o
            número ser bloqueado — e nenhum ajuste aqui torna o bloqueio impossível.
          </p>
        </div>

        <div className="grid gap-3 sm:grid-cols-2">
          <Campo label="Intervalo entre envios (segundos)" hint="Sorteado dentro da faixa, para o ritmo não ficar mecânico.">
            <div className="flex items-center gap-2">
              <Input
                type="number"
                min={1}
                value={c.minIntervalSec}
                onChange={(e) => set({ minIntervalSec: Number(e.target.value) })}
                onBlur={() => void salvar().catch(() => {})}
                disabled={rodando}
                className="h-9"
              />
              <span className="text-xs text-muted-foreground">até</span>
              <Input
                type="number"
                min={2}
                value={c.maxIntervalSec}
                onChange={(e) => set({ maxIntervalSec: Number(e.target.value) })}
                onBlur={() => void salvar().catch(() => {})}
                disabled={rodando}
                className="h-9"
              />
            </div>
          </Campo>

          <Campo label="Teto por número" hint="Por hora e por dia. Zero tira o limite — não recomendo.">
            <div className="flex items-center gap-2">
              <Input
                type="number"
                min={0}
                value={c.perHour}
                onChange={(e) => set({ perHour: Number(e.target.value) })}
                onBlur={() => void salvar().catch(() => {})}
                disabled={rodando}
                className="h-9"
              />
              <span className="whitespace-nowrap text-xs text-muted-foreground">/hora</span>
              <Input
                type="number"
                min={0}
                value={c.perDay}
                onChange={(e) => set({ perDay: Number(e.target.value) })}
                onBlur={() => void salvar().catch(() => {})}
                disabled={rodando}
                className="h-9"
              />
              <span className="whitespace-nowrap text-xs text-muted-foreground">/dia</span>
            </div>
          </Campo>

          <Campo label="Horário permitido" hint="Fora da faixa a campanha dorme e volta sozinha.">
            <div className="flex items-center gap-2">
              <Input
                type="number"
                min={0}
                max={23}
                value={c.windowStart}
                onChange={(e) => set({ windowStart: Number(e.target.value) })}
                onBlur={() => void salvar().catch(() => {})}
                disabled={rodando}
                className="h-9"
              />
              <span className="text-xs text-muted-foreground">às</span>
              <Input
                type="number"
                min={0}
                max={23}
                value={c.windowEnd}
                onChange={(e) => set({ windowEnd: Number(e.target.value) })}
                onBlur={() => void salvar().catch(() => {})}
                disabled={rodando}
                className="h-9"
              />
            </div>
          </Campo>

          <Campo label="Dias da semana">
            <div className="flex flex-wrap gap-1">
              {DIAS.map((d) => {
                const ativos = c.weekdays.split(",").filter(Boolean);
                const marcado = ativos.includes(d.v);
                return (
                  <button
                    key={d.v}
                    type="button"
                    disabled={rodando}
                    onClick={() => {
                      const novo = marcado ? ativos.filter((x) => x !== d.v) : [...ativos, d.v];
                      void salvar({ weekdays: novo.join(",") }).catch(() => {});
                      set({ weekdays: novo.join(",") });
                    }}
                    className={`rounded-md border px-2 py-1 text-[11px] transition ${
                      marcado ? "border-primary bg-primary/10 text-primary" : "text-muted-foreground"
                    }`}
                  >
                    {d.t}
                  </button>
                );
              })}
            </div>
          </Campo>
        </div>

        <label className="flex cursor-pointer items-start gap-2 rounded-lg border bg-muted/20 p-3">
          <input
            type="checkbox"
            checked={c.warmup}
            disabled={rodando}
            onChange={(e) => {
              set({ warmup: e.target.checked });
              void salvar({ warmup: e.target.checked }).catch(() => {});
            }}
            className="mt-0.5 h-3.5 w-3.5"
          />
          <span>
            <span className="block text-xs font-medium">Aquecer números novos</span>
            <span className="block text-[11px] leading-snug text-muted-foreground">
              Um número que nunca disparou começa com 20 mensagens no primeiro dia e vai subindo ao
              longo de uma semana até o seu teto. É o cuidado que mais evita bloqueio em número novo.
            </span>
          </span>
        </label>

        <Campo
          label="Números usados"
          hint={
            conectados.length > 1
              ? "Deixe todos marcados para a campanha alternar entre eles — dividir o volume é o que mais protege cada número."
              : "Você só tem um número conectado. Com dois ou mais, a campanha alterna entre eles e cada um dispara menos."
          }
        >
          <div className="flex flex-wrap gap-1.5">
            {sessions.map((s) => {
              const escolhidos = c.sessionIds.split(",").filter(Boolean);
              const marcado = escolhidos.length === 0 || escolhidos.includes(s.id);
              return (
                <button
                  key={s.id}
                  type="button"
                  disabled={rodando}
                  onClick={() => {
                    const base = escolhidos.length ? escolhidos : sessions.map((x) => x.id);
                    const novo = marcado ? base.filter((x) => x !== s.id) : [...base, s.id];
                    const valor = novo.length === sessions.length ? "" : novo.join(",");
                    set({ sessionIds: valor });
                    void salvar({ sessionIds: valor }).catch(() => {});
                  }}
                  className={`flex items-center gap-1.5 rounded-md border px-2 py-1 text-[11px] transition ${
                    marcado ? "border-primary bg-primary/10" : "text-muted-foreground"
                  }`}
                >
                  <span className={`h-1.5 w-1.5 rounded-full ${s.state === "open" ? "bg-emerald-500" : "bg-muted-foreground/40"}`} />
                  {s.name}
                </button>
              );
            })}
          </div>
        </Campo>
      </section>

      <ConfirmDialog
        open={limpando}
        onOpenChange={setLimpando}
        title="Limpar a lista"
        description={`Os ${prog.total} contatos saem da campanha, inclusive os que já receberam. Não dá para desfazer.`}
        confirmLabel="Limpar"
        onConfirm={async () => {
          await clearTargets(c.id);
          setLimpando(false);
          await recarregar();
        }}
      />
    </div>
  );
};

// ---------------------------------------------------------------------------

export default function CampaignsPage() {
  const [lista, setLista] = useState<CampaignComProgresso[]>([]);
  const [carregando, setCarregando] = useState(true);
  const [aberta, setAberta] = useState("");
  const [aExcluir, setAExcluir] = useState<Campaign | null>(null);

  const recarregar = () =>
    listCampaigns()
      .then(setLista)
      .catch((e) => toast.error((e as Error).message))
      .finally(() => setCarregando(false));

  useEffect(() => {
    if (!aberta) void recarregar();
  }, [aberta]);

  const criar = async () => {
    try {
      const c = await createCampaign({ name: "Nova campanha" });
      setAberta(c.id);
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  if (aberta) {
    return (
      <AppShell>
        <Editor id={aberta} onVoltar={() => setAberta("")} />
      </AppShell>
    );
  }

  return (
    <AppShell>
      <div className="mx-auto w-full max-w-3xl">
        <div className="mb-6 flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="text-xl font-semibold">Campanhas</h1>
            <p className="mt-1 max-w-xl text-sm text-muted-foreground">
              Uma mensagem para uma lista de contatos, disparada devagar e alternando entre os números
              conectados.
            </p>
          </div>
          <Button onClick={criar}>
            <Plus className="mr-1.5 h-4 w-4" />
            Nova campanha
          </Button>
        </div>

        <div className="mb-5 flex items-start gap-2 rounded-xl border border-amber-500/40 bg-amber-500/10 px-3 py-2.5 text-xs leading-snug text-amber-700 dark:text-amber-400">
          <TriangleAlert className="mt-px h-4 w-4 shrink-0" />
          <span>
            O ritmo reduz o risco, mas quem protege o número é a lista: o WhatsApp bloqueia sobretudo
            por denúncia de quem recebe. Dispare para quem pediu contato.
          </span>
        </div>

        {carregando ? (
          <div className="grid place-items-center py-20">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : !lista.length ? (
          <div className="rounded-xl border border-dashed py-16 text-center">
            <Megaphone className="mx-auto mb-3 h-8 w-8 text-muted-foreground/50" />
            <p className="text-sm font-medium">Nenhuma campanha ainda</p>
            <p className="mx-auto mt-1 max-w-sm text-xs text-muted-foreground">
              Crie uma para avisar seus clientes de uma promoção, de um horário novo, ou para mandar o
              catálogo do mês.
            </p>
          </div>
        ) : (
          <div className="divide-y rounded-xl border bg-card">
            {lista.map(({ campaign: c, progress: p }) => (
              <div key={c.id} className="flex items-center gap-3 px-4 py-3">
                <span className={`h-2.5 w-2.5 shrink-0 rounded-full ${COR_STATUS[c.status]}`} />
                <button type="button" onClick={() => setAberta(c.id)} className="min-w-0 flex-1 text-left">
                  <div className="truncate text-sm font-medium">{c.name}</div>
                  <div className="truncate text-xs text-muted-foreground">
                    {ROTULO_STATUS[c.status]}
                    {p.total ? ` · ${p.sent} de ${p.total} enviadas` : " · sem contatos"}
                    {p.failed ? ` · ${p.failed} com erro` : ""}
                  </div>
                </button>
                {p.total ? (
                  <span className="hidden shrink-0 items-center gap-1 text-xs text-muted-foreground sm:flex">
                    <Users className="h-3.5 w-3.5" />
                    {p.total}
                  </span>
                ) : null}
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label="Excluir"
                  className="text-muted-foreground hover:text-destructive"
                  onClick={() => setAExcluir(c)}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>
            ))}
          </div>
        )}
      </div>

      <ConfirmDialog
        open={!!aExcluir}
        onOpenChange={(o) => !o && setAExcluir(null)}
        title="Excluir campanha"
        description={`"${aExcluir?.name}" e a lista de contatos dela serão removidas.`}
        confirmLabel="Excluir"
        onConfirm={async () => {
          if (!aExcluir) return;
          try {
            await deleteCampaign(aExcluir.id);
            setAExcluir(null);
            await recarregar();
          } catch (e) {
            toast.error((e as Error).message);
          }
        }}
      />
    </AppShell>
  );
}
