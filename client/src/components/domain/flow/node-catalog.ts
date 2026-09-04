/**
 * Catálogo dos nós de conversa.
 *
 * Este arquivo é o contrato com o executor em Go (`cmd/server/flowexec_chat.go`).
 * Cada entrada aqui descreve, para um tipo de nó:
 *   - como ele aparece na paleta,
 *   - quais campos o executor lê de `data` (e com que nome exato),
 *   - quantas saídas ele tem e qual `sourceHandle` cada uma usa.
 *
 * Se um campo for renomeado no Go, é aqui que a mudança precisa chegar — o
 * resto da tela lê tudo daqui e não conhece nome de campo nenhum.
 */

import type { FlowNode, FlowNodeData, FlowNodeType } from "@/types/flow";

export type NodeGroup = "conteudo" | "interacao" | "logica" | "sistema" | "integracoes";

/** Uma saída do nó: vira um ponto de conexão no canvas. */
export interface NodeOutput {
  /** Vai no `sourceHandle` da aresta. String vazia = saída única padrão. */
  handle: string;
  label: string;
  tone?: "sim" | "nao" | "neutro";
}

export interface NodeSpec {
  type: FlowNodeType;
  group: NodeGroup;
  label: string;
  /** Uma linha, aparece na paleta e embaixo do título no inspetor. */
  hint: string;
  /** Nome do ícone em lucide-react. */
  icon: string;
  /** Campos gravados em `data` quando o nó é criado. */
  defaults: FlowNodeData;
  /**
   * Saídas fixas. Quando `dynamicOutputs` existe, ela manda e esta lista é
   * ignorada (usada só como fallback para nó sem opção nenhuma).
   */
  outputs: NodeOutput[];
  /** Nós cujas saídas vêm das opções que o usuário cadastrou. */
  dynamicOutputs?: (data: FlowNodeData) => NodeOutput[];
  /** O nó interrompe a execução esperando a resposta do cliente. */
  waits?: boolean;
  /**
   * O executor registra o nó mas ainda não faz a ação de verdade. Melhor dizer
   * isso na cara do usuário do que deixar ele descobrir com o cliente na linha.
   */
  placeholder?: string;
}

export const GROUP_LABEL: Record<NodeGroup, string> = {
  conteudo: "Conteúdo",
  interacao: "Interação",
  logica: "Lógica",
  sistema: "Sistema",
  integracoes: "Integrações",
};

export const GROUP_ORDER: NodeGroup[] = ["conteudo", "interacao", "logica", "sistema", "integracoes"];

const SAIDA_UNICA: NodeOutput[] = [{ handle: "", label: "" }];

/** Opções viram saídas: o handle é a `key`, ou a posição 1-based sem key. */
const outputsDasOpcoes = (data: FlowNodeData): NodeOutput[] => {
  const opts = Array.isArray(data.options) ? data.options : [];
  const saidas = opts.map((o, i) => ({
    handle: String(o?.key ?? "").trim() || String(i + 1),
    label: String(o?.label ?? "").trim() || String(o?.key ?? "") || `Opção ${i + 1}`,
    tone: "neutro" as const,
  }));
  return saidas.length ? saidas : SAIDA_UNICA;
};

export const NODE_SPECS: NodeSpec[] = [
  // ---------------------------------------------------------------- conteúdo
  {
    type: "chat_text",
    group: "conteudo",
    label: "Texto",
    hint: "Mensagem de texto simples",
    icon: "MessageSquare",
    defaults: { text: "" },
    outputs: SAIDA_UNICA,
  },
  {
    type: "chat_content",
    group: "conteudo",
    label: "Conteúdo",
    hint: "Imagem, vídeo, áudio ou documento",
    icon: "Image",
    defaults: { text: "", mediaUrl: "", mediaKind: "image", filename: "" },
    outputs: SAIDA_UNICA,
  },

  // --------------------------------------------------------------- interação
  {
    type: "chat_menu",
    group: "interacao",
    label: "Menu",
    hint: "Menu numerado — o cliente responde com o número",
    icon: "List",
    defaults: {
      prompt: "Escolha uma opção:",
      renderAs: "text",
      options: [
        { key: "1", label: "Vendas" },
        { key: "2", label: "Suporte" },
      ],
      saveAs: "",
    },
    outputs: SAIDA_UNICA,
    dynamicOutputs: outputsDasOpcoes,
    waits: true,
  },
  {
    type: "chat_msg_api",
    group: "interacao",
    label: "Msg interativa",
    hint: "Botões nativos do WhatsApp (até 3) ou lista",
    icon: "MousePointerClick",
    defaults: {
      prompt: "Como podemos te ajudar?",
      footer: "",
      renderAs: "buttons",
      buttonText: "Ver opções",
      options: [
        { key: "comprar", label: "Comprar" },
        { key: "suporte", label: "Suporte" },
      ],
      saveAs: "",
    },
    outputs: SAIDA_UNICA,
    dynamicOutputs: outputsDasOpcoes,
    waits: true,
  },
  {
    type: "chat_input",
    group: "interacao",
    label: "Pergunta aberta",
    hint: "Espera o cliente digitar e guarda a resposta",
    icon: "TextCursorInput",
    defaults: { prompt: "Qual o seu nome?", saveAs: "nome" },
    outputs: SAIDA_UNICA,
    waits: true,
  },
  {
    type: "chat_interval",
    group: "interacao",
    label: "Intervalo",
    hint: "Espera alguns segundos antes de seguir",
    icon: "Clock",
    defaults: { seconds: 2 },
    outputs: SAIDA_UNICA,
  },

  // ------------------------------------------------------------------ lógica
  {
    type: "chat_if_else",
    group: "logica",
    label: "Se / Senão",
    hint: "Separa o caminho conforme uma condição",
    icon: "GitBranch",
    defaults: {
      logic: "and",
      conditions: [{ variable: "message.body", operator: "contains", value: "" }],
    },
    outputs: [
      { handle: "true", label: "Sim", tone: "sim" },
      { handle: "false", label: "Não", tone: "nao" },
    ],
  },
  {
    type: "chat_random",
    group: "logica",
    label: "Randomizador",
    hint: "Sorteia um caminho, com peso por opção",
    icon: "Shuffle",
    defaults: {
      options: [
        { key: "a", label: "Caminho A", weight: 50 },
        { key: "b", label: "Caminho B", weight: 50 },
      ],
    },
    outputs: [
      { handle: "a", label: "A" },
      { handle: "b", label: "B" },
    ],
    dynamicOutputs: outputsDasOpcoes,
  },

  // ----------------------------------------------------------------- sistema
  {
    type: "chat_queue",
    group: "sistema",
    label: "Fila",
    hint: "Direciona o atendimento para uma fila",
    icon: "Users",
    defaults: { queueId: "" },
    outputs: SAIDA_UNICA,
  },
  {
    type: "chat_attendant",
    group: "sistema",
    label: "Atendente",
    hint: "Passa a conversa para uma pessoa",
    icon: "HeartHandshake",
    defaults: { destination: "" },
    outputs: SAIDA_UNICA,
    placeholder:
      "O executor apenas registra a passagem — ainda não existe transferência real para um atendente. " +
      "Use um nó de Fila junto, senão a conversa segue no fluxo.",
  },
  {
    type: "chat_tag_add",
    group: "sistema",
    label: "Marcar tag",
    hint: "Guarda uma tag na conversa",
    icon: "Tag",
    defaults: { tag: "" },
    outputs: SAIDA_UNICA,
  },
  {
    type: "chat_tag_remove",
    group: "sistema",
    label: "Remover tag",
    hint: "Tira uma tag da conversa",
    icon: "Eraser",
    defaults: { tag: "" },
    outputs: SAIDA_UNICA,
  },
  {
    type: "chat_switch_flow",
    group: "sistema",
    label: "Trocar fluxo",
    hint: "Manda a conversa para outro fluxo",
    icon: "Workflow",
    defaults: { flowId: "" },
    outputs: SAIDA_UNICA,
    placeholder:
      "O executor registra a troca mas ainda não carrega o outro fluxo — a conversa continua neste.",
  },

  // ------------------------------------------------------------- integrações
  {
    type: "chat_variable",
    group: "integracoes",
    label: "Variável",
    hint: "Guarda um valor para usar depois",
    icon: "Variable",
    defaults: { variable: "", value: "" },
    outputs: SAIDA_UNICA,
  },
  {
    type: "chat_http",
    group: "integracoes",
    label: "Requisição HTTP",
    hint: "Chama uma API e guarda a resposta",
    icon: "Globe",
    defaults: { method: "GET", url: "", body: "", headers: "", saveAs: "apiResponse", responseMap: [] },
    outputs: SAIDA_UNICA,
  },
  {
    type: "chat_n8n",
    group: "integracoes",
    label: "n8n",
    hint: "Dispara um webhook do n8n",
    icon: "Boxes",
    defaults: { url: "", method: "POST", body: "", authHeader: "", responseVariable: "n8n" },
    outputs: SAIDA_UNICA,
  },
  {
    type: "chat_ai_agent",
    group: "integracoes",
    label: "Agente de IA",
    hint: "Entrega a conversa para um agente de IA",
    icon: "Bot",
    defaults: { agentId: "" },
    outputs: SAIDA_UNICA,
    placeholder: "O executor registra a passagem, mas o agente de IA ainda não está ligado.",
  },
];

export const SPEC_BY_TYPE: Record<string, NodeSpec> = Object.fromEntries(
  NODE_SPECS.map((s) => [s.type, s]),
);

export const specOf = (type: string): NodeSpec | undefined => SPEC_BY_TYPE[type];

/** As saídas de um nó já montado — o que o canvas desenha e o que as arestas usam. */
export const outputsOf = (node: Pick<FlowNode, "type" | "data">): NodeOutput[] => {
  const spec = specOf(node.type);
  if (!spec) return SAIDA_UNICA;
  return spec.dynamicOutputs ? spec.dynamicOutputs(node.data ?? {}) : spec.outputs;
};

let contador = 0;
export const novoIdDeNo = (type: string) =>
  `${type}_${Date.now().toString(36)}${(contador++).toString(36)}`;

export const criarNo = (type: FlowNodeType, position: { x: number; y: number }): FlowNode => {
  const spec = specOf(type);
  return {
    id: novoIdDeNo(type),
    type,
    position,
    // Cópia funda: sem isso, dois nós do mesmo tipo passam a dividir a mesma
    // lista de opções e editar um altera o outro.
    data: { ...(spec ? JSON.parse(JSON.stringify(spec.defaults)) : {}) },
  };
};
