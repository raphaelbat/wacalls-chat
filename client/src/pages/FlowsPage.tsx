/**
 * Lista de fluxos de conversa.
 *
 * Daqui se cria, duplica, ativa e abre o construtor. Só mostra fluxos de
 * conversa — os de voz (URA) usam o mesmo armazenamento mas outra tela.
 */

import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Copy, Loader2, Plus, Trash2, Workflow } from "lucide-react";
import { toast } from "sonner";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import { createFlow, deleteFlow, duplicateFlow, listFlows, parseGraph, serializeGraph, updateFlow } from "@/services/flows";
import type { FlowGraph, FlowRow } from "@/types/flow";
import { criarNo } from "@/components/domain/flow/node-catalog";

const ehDeConversa = (f: FlowRow) => parseGraph(f.graph).kind === "chat";

/** Fluxo novo já nasce com uma saudação: tela em branco não ensina nada. */
const grafoInicial = (): FlowGraph => {
  const saudacao = criarNo("chat_text", { x: 80, y: 120 });
  saudacao.data = { text: "Olá! 👋 Como podemos te ajudar?" };
  const menu = criarNo("chat_msg_api", { x: 420, y: 100 });
  return {
    nodes: [saudacao, menu],
    edges: [{ id: `e_${saudacao.id}_${menu.id}`, source: saudacao.id, target: menu.id, sourceHandle: "" }],
    startNodeId: saudacao.id,
    kind: "chat",
  };
};

export default function FlowsPage() {
  const navigate = useNavigate();
  const [fluxos, setFluxos] = useState<FlowRow[]>([]);
  const [carregando, setCarregando] = useState(true);
  const [criando, setCriando] = useState(false);
  const [aExcluir, setAExcluir] = useState<FlowRow | null>(null);

  const recarregar = () =>
    listFlows()
      .then(setFluxos)
      .catch((e) => toast.error((e as Error).message))
      .finally(() => setCarregando(false));

  useEffect(() => {
    void recarregar();
  }, []);

  const deConversa = useMemo(() => fluxos.filter(ehDeConversa), [fluxos]);

  const criar = async () => {
    setCriando(true);
    try {
      const f = await createFlow({ name: "Novo fluxo de conversa", trigger: "inbound" });
      await updateFlow(f.id, { graph: serializeGraph(grafoInicial()) });
      navigate(`/flows/${f.id}`);
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setCriando(false);
    }
  };

  const alternar = async (f: FlowRow) => {
    try {
      await updateFlow(f.id, { enabled: !f.enabled });
      await recarregar();
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  return (
    <AppShell>
      <div className="mx-auto w-full max-w-5xl">
        <div className="mb-6 flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="text-xl font-semibold">Fluxos de conversa</h1>
            <p className="mt-1 max-w-xl text-sm text-muted-foreground">
              Monte o atendimento automático e vincule a um número em <strong>Conexões</strong>. Quando o cliente
              mandar mensagem, a conversa entra no fluxo escolhido.
            </p>
          </div>
          <Button onClick={criar} disabled={criando}>
            {criando ? <Loader2 className="mr-1.5 h-4 w-4 animate-spin" /> : <Plus className="mr-1.5 h-4 w-4" />}
            Novo fluxo
          </Button>
        </div>

        {carregando ? (
          <div className="grid place-items-center py-20">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : !deConversa.length ? (
          <div className="rounded-xl border border-dashed py-16 text-center">
            <Workflow className="mx-auto mb-3 h-8 w-8 text-muted-foreground/50" />
            <p className="text-sm font-medium">Nenhum fluxo ainda</p>
            <p className="mx-auto mt-1 max-w-sm text-xs text-muted-foreground">
              Crie o primeiro para responder sozinho quem chamar fora do horário, ou para perguntar o assunto antes
              de passar para um atendente.
            </p>
          </div>
        ) : (
          <div className="divide-y rounded-xl border bg-card">
            {deConversa.map((f) => {
              const g = parseGraph(f.graph);
              return (
                <div key={f.id} className="flex items-center gap-3 px-4 py-3">
                  <button
                    type="button"
                    onClick={() => void alternar(f)}
                    title={f.enabled ? "Desativar" : "Ativar"}
                    className={`h-2.5 w-2.5 shrink-0 rounded-full transition ${
                      f.enabled ? "bg-emerald-500" : "bg-muted-foreground/40"
                    }`}
                  />
                  <button
                    type="button"
                    onClick={() => navigate(`/flows/${f.id}`)}
                    className="min-w-0 flex-1 text-left"
                  >
                    <div className="truncate text-sm font-medium">{f.name}</div>
                    <div className="truncate text-xs text-muted-foreground">
                      {g.nodes.length} passo{g.nodes.length === 1 ? "" : "s"}
                      {f.keywords ? ` · responde a: ${f.keywords}` : " · sem palavra-chave"}
                      {f.enabled ? "" : " · desativado"}
                    </div>
                  </button>
                  <Button variant="ghost" size="icon" aria-label="Duplicar" onClick={() => void duplicateFlow(f.id).then(recarregar)}>
                    <Copy className="h-4 w-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="Excluir"
                    className="text-muted-foreground hover:text-destructive"
                    onClick={() => setAExcluir(f)}
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
              );
            })}
          </div>
        )}
      </div>

      <ConfirmDialog
        open={!!aExcluir}
        onOpenChange={(o) => !o && setAExcluir(null)}
        title="Excluir fluxo"
        description={`"${aExcluir?.name}" será removido. Conexões vinculadas a ele deixam de responder sozinhas.`}
        confirmLabel="Excluir"
        onConfirm={async () => {
          if (!aExcluir) return;
          try {
            await deleteFlow(aExcluir.id);
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
