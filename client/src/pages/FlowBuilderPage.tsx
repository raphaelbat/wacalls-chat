/**
 * Construtor de fluxo de conversa.
 *
 * Monta o grafo que o executor do servidor roda quando chega mensagem no número
 * vinculado. O JSON gravado segue o contrato de `node-catalog.ts`.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import {
  addEdge,
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
  useEdgesState,
  useNodesState,
  useReactFlow,
  type Connection,
  type Edge,
  type Node,
  type NodeTypes,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import * as Icons from "lucide-react";
import { ArrowLeft, Loader2, Save, TriangleAlert } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { AppShell } from "@/components/layout/AppShell";
import { FlowNodeCard } from "@/components/domain/flow/FlowNodeCard";
import { NodeInspector } from "@/components/domain/flow/NodeInspector";
import { FlowSimulator } from "@/components/domain/flow/FlowSimulator";
import { saidasSoltas } from "@/components/domain/flow/simulator";
import {
  criarNo,
  GROUP_LABEL,
  GROUP_ORDER,
  NODE_SPECS,
  outputsOf,
  specOf,
} from "@/components/domain/flow/node-catalog";
import { getFlow, listFlows, parseGraph, serializeGraph, updateFlow } from "@/services/flows";
import type { FlowGraph, FlowNode, FlowNodeData, FlowNodeType, FlowRow } from "@/types/flow";

const nodeTypes: NodeTypes = Object.fromEntries(
  NODE_SPECS.map((s) => [s.type, FlowNodeCard]),
) as NodeTypes;

type RFNode = Node<{ data: FlowNodeData; ehInicio?: boolean; visitado?: boolean }>;

const paraCanvas = (n: FlowNode, inicio: string): RFNode => ({
  id: n.id,
  type: n.type,
  position: n.position ?? { x: 0, y: 0 },
  data: { data: n.data ?? {}, ehInicio: n.id === inicio },
});

const doCanvas = (n: RFNode): FlowNode => ({
  id: n.id,
  type: (n.type ?? "chat_text") as FlowNodeType,
  position: n.position,
  data: n.data.data ?? {},
});

// ---------------------------------------------------------------------------

const Paleta = ({ onAdicionar }: { onAdicionar: (t: FlowNodeType) => void }) => (
  <aside className="w-56 shrink-0 overflow-y-auto border-r bg-card/40">
    <div className="px-3 py-3 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
      Adicionar passo
    </div>
    {GROUP_ORDER.map((grupo) => {
      const doGrupo = NODE_SPECS.filter((s) => s.group === grupo);
      if (!doGrupo.length) return null;
      return (
        <div key={grupo} className="px-2 pb-3">
          <div className="px-1 pb-1.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground/70">
            {GROUP_LABEL[grupo]}
          </div>
          <div className="space-y-1">
            {doGrupo.map((s) => {
              const Icone =
                (Icons as unknown as Record<string, React.ComponentType<{ className?: string }>>)[s.icon] ??
                Icons.Circle;
              return (
                <button
                  key={s.type}
                  type="button"
                  draggable
                  onDragStart={(e) => {
                    e.dataTransfer.setData("application/wacalls-node", s.type);
                    e.dataTransfer.effectAllowed = "move";
                  }}
                  onClick={() => onAdicionar(s.type)}
                  className="flex w-full items-start gap-2 rounded-lg border bg-background px-2 py-1.5 text-left transition hover:border-primary/50 hover:bg-accent"
                >
                  <span className="mt-0.5 grid h-6 w-6 shrink-0 place-items-center rounded-md bg-muted text-muted-foreground">
                    <Icone className="h-3.5 w-3.5" />
                  </span>
                  <span className="min-w-0">
                    <span className="block truncate text-xs font-medium">{s.label}</span>
                    <span className="block truncate text-[10px] leading-tight text-muted-foreground">{s.hint}</span>
                  </span>
                </button>
              );
            })}
          </div>
        </div>
      );
    })}
  </aside>
);

// ---------------------------------------------------------------------------

const Editor = ({ flow, fluxos }: { flow: FlowRow; fluxos: FlowRow[] }) => {
  const navigate = useNavigate();
  const inicial = useMemo(() => parseGraph(flow.graph), [flow.graph]);
  const [startNodeId, setStartNodeId] = useState(inicial.startNodeId || inicial.nodes[0]?.id || "");
  const [nodes, setNodes, onNodesChange] = useNodesState<RFNode>(
    inicial.nodes.map((n) => paraCanvas(n, inicial.startNodeId || inicial.nodes[0]?.id || "")),
  );
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>(
    inicial.edges.map((e) => ({ id: e.id, source: e.source, target: e.target, sourceHandle: e.sourceHandle || null })),
  );
  const [nome, setNome] = useState(flow.name);
  const [palavras, setPalavras] = useState(flow.keywords ?? "");
  const [ativo, setAtivo] = useState(flow.enabled);
  const [selecionado, setSelecionado] = useState<string>("");
  const [aba, setAba] = useState<"config" | "simulador">("config");
  const [salvando, setSalvando] = useState(false);
  const [sujo, setSujo] = useState(false);
  const wrapper = useRef<HTMLDivElement>(null);
  const { screenToFlowPosition } = useReactFlow();

  useEffect(() => setSujo(true), [nodes, edges, nome, palavras, ativo, startNodeId]);

  const grafo: FlowGraph = useMemo(
    () => ({
      nodes: (nodes as RFNode[]).map(doCanvas),
      edges: edges.map((e) => ({
        id: e.id,
        source: e.source,
        target: e.target,
        sourceHandle: e.sourceHandle ?? "",
      })),
      startNodeId: startNodeId || (nodes[0]?.id ?? ""),
      // Sem isto o servidor trata o fluxo como URA de voz e ele nunca dispara
      // por mensagem. É o campo mais fácil de esquecer e o mais caro.
      kind: "chat",
    }),
    [nodes, edges, startNodeId],
  );

  const soltas = useMemo(() => saidasSoltas(grafo), [grafo]);

  const adicionar = useCallback(
    (tipo: FlowNodeType, pos?: { x: number; y: number }) => {
      const novo = criarNo(tipo, pos ?? { x: 120 + Math.random() * 220, y: 100 + Math.random() * 220 });
      setNodes((ns) => [...ns, paraCanvas(novo, startNodeId || novo.id)]);
      if (!startNodeId) setStartNodeId(novo.id);
      setSelecionado(novo.id);
      setAba("config");
    },
    [setNodes, startNodeId],
  );

  const onConnect = useCallback(
    (c: Connection) =>
      setEdges((es) =>
        addEdge(
          { ...c, id: `e_${c.source}_${c.sourceHandle ?? ""}_${c.target}_${Date.now().toString(36)}` },
          // Uma saída leva a um passo só: reconectar troca o destino em vez de
          // criar um segundo caminho invisível a partir do mesmo ponto.
          es.filter((e) => !(e.source === c.source && (e.sourceHandle ?? "") === (c.sourceHandle ?? ""))),
        ),
      ),
    [setEdges],
  );

  const atualizarDados = useCallback(
    (id: string, data: FlowNodeData) => {
      setNodes((ns) => ns.map((n) => (n.id === id ? { ...n, data: { ...n.data, data } } : n)));
      // Mudar as opções muda as saídas: arestas que apontavam para uma saída que
      // não existe mais viram linhas fantasmas se ficarem.
      const no = (nodes as RFNode[]).find((n) => n.id === id);
      if (no) {
        const validos = new Set(outputsOf({ type: no.type as never, data }).map((s) => s.handle));
        setEdges((es) => es.filter((e) => e.source !== id || validos.has(e.sourceHandle ?? "")));
      }
    },
    [nodes, setEdges, setNodes],
  );

  const remover = useCallback(
    (id: string) => {
      setNodes((ns) => ns.filter((n) => n.id !== id));
      setEdges((es) => es.filter((e) => e.source !== id && e.target !== id));
      if (selecionado === id) setSelecionado("");
      if (startNodeId === id) setStartNodeId("");
    },
    [selecionado, setEdges, setNodes, startNodeId],
  );

  useEffect(() => {
    setNodes((ns) => ns.map((n) => ({ ...n, data: { ...n.data, ehInicio: n.id === startNodeId } })));
  }, [startNodeId, setNodes]);

  const salvar = async () => {
    if (!grafo.nodes.length) {
      toast.error("Adicione ao menos um passo antes de salvar.");
      return;
    }
    setSalvando(true);
    try {
      await updateFlow(flow.id, {
        name: nome.trim() || "Fluxo sem nome",
        trigger: flow.trigger,
        graph: serializeGraph(grafo),
        enabled: ativo,
        keywords: palavras,
        keywordMatch: flow.keywordMatch ?? "any",
      });
      setSujo(false);
      toast.success("Fluxo salvo");
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setSalvando(false);
    }
  };

  const noSelecionado = (nodes as RFNode[]).find((n) => n.id === selecionado);

  return (
    <div className="flex h-[calc(100dvh-7rem)] flex-col overflow-hidden rounded-xl border bg-background">
      {/* ------------------------------------------------------------ topo */}
      <div className="flex flex-wrap items-center gap-2 border-b px-3 py-2.5">
        <Button variant="ghost" size="sm" onClick={() => navigate("/flows")}>
          <ArrowLeft className="mr-1.5 h-4 w-4" />
          Fluxos
        </Button>
        <Input
          value={nome}
          onChange={(e) => setNome(e.target.value)}
          className="h-9 w-56 text-sm font-medium"
          placeholder="Nome do fluxo"
        />
        <div className="flex items-center gap-1.5">
          <Label htmlFor="kw" className="text-xs text-muted-foreground">
            Palavras-chave
          </Label>
          <Input
            id="kw"
            value={palavras}
            onChange={(e) => setPalavras(e.target.value)}
            className="h-9 w-52 text-sm"
            placeholder="oi, menu, ajuda"
          />
        </div>
        <label className="flex cursor-pointer items-center gap-1.5 text-xs">
          <input type="checkbox" checked={ativo} onChange={(e) => setAtivo(e.target.checked)} className="h-3.5 w-3.5" />
          Ativo
        </label>

        <div className="ml-auto flex items-center gap-2">
          {sujo ? <span className="text-[11px] text-muted-foreground">alterações não salvas</span> : null}
          <Button size="sm" onClick={salvar} disabled={salvando}>
            {salvando ? <Loader2 className="mr-1.5 h-4 w-4 animate-spin" /> : <Save className="mr-1.5 h-4 w-4" />}
            Salvar
          </Button>
        </div>
      </div>

      <div className="flex min-h-0 flex-1">
        <Paleta onAdicionar={(t) => adicionar(t)} />

        {/* -------------------------------------------------------- canvas */}
        <div className="relative min-w-0 flex-1" ref={wrapper}>
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            nodeTypes={nodeTypes}
            onNodeClick={(_, n) => {
              setSelecionado(n.id);
              setAba("config");
            }}
            onPaneClick={() => setSelecionado("")}
            onDragOver={(e) => {
              e.preventDefault();
              e.dataTransfer.dropEffect = "move";
            }}
            onDrop={(e) => {
              e.preventDefault();
              const tipo = e.dataTransfer.getData("application/wacalls-node") as FlowNodeType;
              if (!tipo || !specOf(tipo)) return;
              adicionar(tipo, screenToFlowPosition({ x: e.clientX, y: e.clientY }));
            }}
            fitView
            proOptions={{ hideAttribution: false }}
            defaultEdgeOptions={{ animated: true, style: { strokeWidth: 1.5 } }}
          >
            <Background variant={BackgroundVariant.Dots} gap={16} size={1} />
            <Controls showInteractive={false} />
            <MiniMap
              pannable
              zoomable
              // Sem tamanho e borda o mapa vira um retângulo branco solto no
              // canto, que parece defeito em vez de recurso.
              style={{ width: 132, height: 88 }}
              className="!rounded-lg !border !bg-card"
              maskColor="rgb(148 163 184 / 0.18)"
              nodeColor="rgb(148 163 184 / 0.75)"
              nodeStrokeWidth={0}
            />
          </ReactFlow>

          {!nodes.length ? (
            <div className="pointer-events-none absolute inset-0 grid place-items-center">
              <p className="max-w-xs text-center text-sm text-muted-foreground">
                Arraste um passo da esquerda para começar. O primeiro passo é por onde a conversa entra.
              </p>
            </div>
          ) : null}

          {soltas.length ? (
            <div className="pointer-events-none absolute bottom-3 left-3 flex items-start gap-1.5 rounded-lg border border-amber-500/40 bg-amber-500/10 px-2.5 py-1.5 text-[11px] text-amber-700 dark:text-amber-400">
              <TriangleAlert className="mt-px h-3.5 w-3.5 shrink-0" />
              <span>
                {soltas.length} saída{soltas.length > 1 ? "s" : ""} sem destino — a conversa para nesses pontos.
              </span>
            </div>
          ) : null}
        </div>

        {/* ------------------------------------------------------- inspetor */}
        <aside className="flex w-80 shrink-0 flex-col border-l bg-card/40">
          <div className="flex border-b">
            {(["config", "simulador"] as const).map((k) => (
              <button
                key={k}
                type="button"
                onClick={() => setAba(k)}
                className={`flex-1 px-3 py-2 text-xs font-medium transition ${
                  aba === k ? "border-b-2 border-primary text-foreground" : "text-muted-foreground hover:text-foreground"
                }`}
              >
                {k === "config" ? "Propriedades" : "Simulador"}
              </button>
            ))}
          </div>

          <div className="min-h-0 flex-1">
            {aba === "simulador" ? (
              <FlowSimulator graph={grafo} onNoAtivo={setSelecionado} />
            ) : noSelecionado ? (
              <NodeInspector
                node={doCanvas(noSelecionado)}
                fluxos={fluxos.filter((f) => f.id !== flow.id)}
                ehInicio={startNodeId === noSelecionado.id}
                onChange={(d) => atualizarDados(noSelecionado.id, d)}
                onDefinirInicio={() => setStartNodeId(noSelecionado.id)}
                onRemover={() => remover(noSelecionado.id)}
              />
            ) : (
              <p className="px-4 py-8 text-center text-xs text-muted-foreground">
                Clique num passo do fluxo para editar o que ele faz.
              </p>
            )}
          </div>
        </aside>
      </div>
    </div>
  );
};

// ---------------------------------------------------------------------------

export default function FlowBuilderPage() {
  const { id = "" } = useParams();
  const [flow, setFlow] = useState<FlowRow | null>(null);
  const [fluxos, setFluxos] = useState<FlowRow[]>([]);
  const [erro, setErro] = useState("");

  useEffect(() => {
    let vivo = true;
    void getFlow(id)
      .then((f) => vivo && setFlow(f))
      .catch((e) => vivo && setErro((e as Error).message));
    void listFlows()
      .then((f) => vivo && setFluxos(f))
      .catch(() => {});
    return () => {
      vivo = false;
    };
  }, [id]);

  return (
    <AppShell>
      {erro ? (
        <p className="py-16 text-center text-sm text-destructive">Não consegui abrir este fluxo: {erro}</p>
      ) : !flow ? (
        <div className="grid place-items-center py-16">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </div>
      ) : (
        <ReactFlowProvider>
          <Editor flow={flow} fluxos={fluxos} />
        </ReactFlowProvider>
      )}
    </AppShell>
  );
}
