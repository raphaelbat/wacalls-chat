/**
 * Simulador do fluxo de conversa.
 *
 * Roda o mesmo grafo que o servidor roda, mas aqui no navegador, para você
 * conferir o caminho antes de ligar no WhatsApp. Ele imita as regras do
 * executor Go (`cmd/server/flowexec_chat.go`): qual saída cada nó escolhe, como
 * a resposta do cliente casa com as opções, e onde a conversa fica esperando.
 *
 * O que ele NÃO faz, de propósito: enviar mensagem de verdade, chamar API
 * externa e falar com fila ou atendente. Esses passos aparecem marcados como
 * "não executado aqui", para a simulação nunca dar a impressão de que algo
 * aconteceu no mundo real.
 */

import type { FlowEdge, FlowGraph, FlowNode, FlowNodeData } from "@/types/flow";
import { outputsOf } from "./node-catalog";

export interface SimBotao {
  id: string;
  label: string;
}

export interface SimMensagem {
  id: number;
  autor: "bot" | "cliente" | "sistema";
  texto: string;
  /** Só em mensagens do bot que esperam escolha. */
  botoes?: SimBotao[];
  /** Nó que gerou a linha — usado para destacar no canvas. */
  nodeId?: string;
  /** Passo que não roda na simulação (API, fila, atendente). */
  aviso?: boolean;
}

export interface SimEstado {
  mensagens: SimMensagem[];
  /** Nó onde parou esperando resposta; vazio quando terminou. */
  esperandoEm: string;
  esperandoTipo: "menu" | "input" | "";
  vars: Record<string, unknown>;
  encerrado: boolean;
  /** Caminho percorrido, para desenhar no canvas. */
  visitados: string[];
}

const MAX_PASSOS = 200;

// ---------------------------------------------------------------------------
// Variáveis e templates
// ---------------------------------------------------------------------------

/** Resolve "vars.nome" ou "message.body" dentro do mapa raiz. */
export const lookupVar = (raiz: Record<string, unknown>, caminho: string): string => {
  const partes = String(caminho || "").split(".").filter(Boolean);
  let atual: unknown = raiz;
  for (const p of partes) {
    if (atual && typeof atual === "object" && p in (atual as Record<string, unknown>)) {
      atual = (atual as Record<string, unknown>)[p];
    } else {
      return "";
    }
  }
  if (atual === null || atual === undefined) return "";
  return typeof atual === "object" ? JSON.stringify(atual) : String(atual);
};

/**
 * O servidor usa text/template do Go, então a sintaxe tem o ponto na frente:
 * {{.vars.nome}}. Aceitamos também {{vars.nome}} porque é o erro mais comum e
 * não custa nada perdoar — mas o inspetor sempre sugere a forma com ponto.
 */
export const renderTemplate = (texto: string, raiz: Record<string, unknown>): string =>
  String(texto ?? "").replace(/\{\{\s*\.?([A-Za-z0-9_.]+)\s*\}\}/g, (_m, caminho: string) =>
    lookupVar(raiz, caminho),
  );

// ---------------------------------------------------------------------------
// Grafo
// ---------------------------------------------------------------------------

const acharNo = (g: FlowGraph, id: string): FlowNode | undefined => g.nodes.find((n) => n.id === id);

/** Mesma regra do executor: handle exato, senão "" ou "out", senão a primeira. */
export const proximoNo = (g: FlowGraph, nodeId: string, handle: string): string => {
  const saindo = g.edges.filter((e: FlowEdge) => e.source === nodeId);
  if (handle) {
    const exata = saindo.find((e) => (e.sourceHandle ?? "") === handle);
    if (exata) return exata.target;
  } else {
    const padrao = saindo.find((e) => !e.sourceHandle || e.sourceHandle === "out");
    if (padrao) return padrao.target;
  }
  return saindo[0]?.target ?? "";
};

const paraNumero = (v: unknown, padrao = 0): number => {
  const n = typeof v === "number" ? v : parseFloat(String(v ?? ""));
  return Number.isFinite(n) ? n : padrao;
};

const opcoesDe = (data: FlowNodeData): Array<{ key: string; label: string; weight?: number }> =>
  (Array.isArray(data.options) ? data.options : []).map((o, i) => ({
    key: String(o?.key ?? "").trim() || String(i + 1),
    label: String(o?.label ?? "").trim() || String(o?.key ?? "") || `Opção ${i + 1}`,
    weight: paraNumero((o as { weight?: unknown })?.weight, 1),
  }));

// ---------------------------------------------------------------------------
// Condições
// ---------------------------------------------------------------------------

const avaliarUma = (raiz: Record<string, unknown>, c: { variable?: string; operator?: string; value?: string }) => {
  const atual = lookupVar(raiz, String(c.variable ?? ""));
  const esperado = String(c.value ?? "");
  switch (String(c.operator ?? "eq")) {
    case "eq":
    case "==":
    case "":
      return atual === esperado;
    case "neq":
    case "!=":
      return atual !== esperado;
    case "contains":
      return atual.toLowerCase().includes(esperado.toLowerCase());
    case "starts_with":
      return atual.toLowerCase().startsWith(esperado.toLowerCase());
    case "empty":
      return atual === "";
    case "not_empty":
      return atual !== "";
    default:
      // O executor devolve false para operador que não conhece.
      return false;
  }
};

const avaliarCondicoes = (raiz: Record<string, unknown>, data: FlowNodeData): boolean => {
  const lista = Array.isArray(data.conditions) ? data.conditions : [];
  if (!lista.length) {
    return avaliarUma(raiz, {
      variable: String(data.variable ?? ""),
      operator: String(data.operator ?? "eq"),
      value: String(data.value ?? ""),
    });
  }
  const eOu = String(data.logic ?? "and").toLowerCase();
  const resultados = lista.map((c) => avaliarUma(raiz, c as Record<string, string>));
  return eOu === "or" ? resultados.some(Boolean) : resultados.every(Boolean);
};

// ---------------------------------------------------------------------------
// Execução
// ---------------------------------------------------------------------------

export const estadoInicial = (telefone = "+55 81 90000-0000"): SimEstado => ({
  mensagens: [],
  esperandoEm: "",
  esperandoTipo: "",
  vars: {
    vars: {},
    message: { body: "", chat: telefone, from: telefone, kind: "text", fromMe: false, isGroup: false },
    call: { from: telefone, session: "simulador", id: "sim" },
    flow: { id: "sim", owner: "" },
  },
  encerrado: false,
  visitados: [],
});

let seq = 0;
const msg = (m: Omit<SimMensagem, "id">): SimMensagem => ({ id: ++seq, ...m });

const setVar = (estado: SimEstado, chave: string, valor: unknown) => {
  const v = estado.vars.vars as Record<string, unknown>;
  v[chave] = valor;
};

/** Roda do nó indicado até parar (espera do cliente, fim ou erro). */
export function executar(g: FlowGraph, estado: SimEstado, comecarEm: string): SimEstado {
  let atual = comecarEm;
  let passos = 0;

  while (atual && passos < MAX_PASSOS) {
    passos++;
    const no = acharNo(g, atual);
    if (!no) {
      estado.mensagens.push(msg({ autor: "sistema", texto: `Nó "${atual}" não existe no fluxo.`, aviso: true }));
      break;
    }
    estado.visitados.push(no.id);
    const d = (no.data ?? {}) as FlowNodeData;
    const t = (s: unknown) => renderTemplate(String(s ?? ""), estado.vars);

    switch (no.type) {
      case "chat_text":
      case "chat_content": {
        const corpo = t(d.text || d.prompt || d.template);
        if (d.mediaUrl) {
          estado.mensagens.push(
            msg({
              autor: "bot",
              nodeId: no.id,
              texto: `[${String(d.mediaKind || "image")}] ${t(d.mediaUrl)}${corpo ? `\n${corpo}` : ""}`,
            }),
          );
        } else if (corpo) {
          estado.mensagens.push(msg({ autor: "bot", nodeId: no.id, texto: corpo }));
        } else {
          estado.mensagens.push(
            msg({ autor: "sistema", nodeId: no.id, texto: "Nó de texto sem conteúdo — nada é enviado.", aviso: true }),
          );
        }
        atual = proximoNo(g, no.id, "");
        break;
      }

      case "chat_menu":
      case "chat_msg_api":
      case "chat_input": {
        const corpo = t(d.prompt || d.text);
        const opts = no.type === "chat_input" ? [] : opcoesDe(d);
        if (!corpo && !opts.length) {
          estado.mensagens.push(
            msg({ autor: "sistema", nodeId: no.id, texto: "Nó de pergunta sem texto e sem opções — é ignorado.", aviso: true }),
          );
          atual = proximoNo(g, no.id, "");
          break;
        }
        const texto = d.footer ? `${corpo}\n\n${t(d.footer)}` : corpo;
        estado.mensagens.push(
          msg({
            autor: "bot",
            nodeId: no.id,
            texto,
            botoes: opts.map((o) => ({ id: o.key, label: o.label })),
          }),
        );
        estado.esperandoEm = no.id;
        estado.esperandoTipo = no.type === "chat_input" ? "input" : "menu";
        return estado;
      }

      case "chat_interval": {
        const s = paraNumero(d.seconds, 1) || 1;
        estado.mensagens.push(msg({ autor: "sistema", nodeId: no.id, texto: `Espera de ${s}s (pulada na simulação).` }));
        atual = proximoNo(g, no.id, "");
        break;
      }

      case "chat_if_else": {
        const ok = avaliarCondicoes(estado.vars, d);
        estado.mensagens.push(
          msg({ autor: "sistema", nodeId: no.id, texto: `Condição avaliada: ${ok ? "sim" : "não"}.` }),
        );
        atual = proximoNo(g, no.id, ok ? "true" : "false");
        break;
      }

      case "chat_random": {
        const opts = opcoesDe(d);
        let escolhida = Math.random() < 0.5 ? "a" : "b";
        if (opts.length) {
          const total = opts.reduce((s, o) => s + Math.max(0, o.weight ?? 1), 0) || opts.length;
          let sorteio = Math.random() * total;
          escolhida = opts[opts.length - 1].key;
          for (const o of opts) {
            sorteio -= Math.max(0, o.weight ?? 1);
            if (sorteio <= 0) {
              escolhida = o.key;
              break;
            }
          }
        }
        estado.mensagens.push(msg({ autor: "sistema", nodeId: no.id, texto: `Sorteou o caminho "${escolhida}".` }));
        atual = proximoNo(g, no.id, escolhida);
        break;
      }

      case "chat_variable": {
        const nome = String(d.variable ?? "").trim();
        const valor = t(d.value);
        if (nome) setVar(estado, nome, valor);
        estado.mensagens.push(
          msg({ autor: "sistema", nodeId: no.id, texto: nome ? `${nome} = ${valor || "(vazio)"}` : "Variável sem nome — ignorada." }),
        );
        atual = proximoNo(g, no.id, "");
        break;
      }

      case "chat_queue": {
        const fila = t(d.queueId) || String(d.queue ?? "");
        setVar(estado, "queue", fila);
        estado.mensagens.push(
          msg({ autor: "sistema", nodeId: no.id, texto: `Direcionado para a fila "${fila || "(nenhuma)"}".` }),
        );
        atual = proximoNo(g, no.id, "");
        break;
      }

      case "chat_tag_add":
      case "chat_tag_remove": {
        const tag = t(d.tag);
        const acao = no.type === "chat_tag_add" ? "Marcou" : "Removeu";
        setVar(estado, no.type === "chat_tag_add" ? "last_tag_added" : "last_tag_removed", tag);
        estado.mensagens.push(msg({ autor: "sistema", nodeId: no.id, texto: `${acao} a tag "${tag || "(vazia)"}".` }));
        atual = proximoNo(g, no.id, "");
        break;
      }

      case "chat_attendant": {
        estado.mensagens.push(
          msg({
            autor: "sistema",
            nodeId: no.id,
            aviso: true,
            texto: "Passagem para atendente registrada — o executor ainda não transfere de verdade, a conversa segue no fluxo.",
          }),
        );
        atual = proximoNo(g, no.id, "");
        break;
      }

      case "chat_switch_flow": {
        estado.mensagens.push(
          msg({
            autor: "sistema",
            nodeId: no.id,
            aviso: true,
            texto: "Troca de fluxo registrada — o executor ainda não carrega o outro fluxo.",
          }),
        );
        atual = proximoNo(g, no.id, "");
        break;
      }

      case "chat_ai_agent": {
        estado.mensagens.push(
          msg({ autor: "sistema", nodeId: no.id, aviso: true, texto: "Agente de IA ainda não está ligado no executor." }),
        );
        atual = proximoNo(g, no.id, "");
        break;
      }

      case "chat_http":
      case "chat_n8n": {
        estado.mensagens.push(
          msg({
            autor: "sistema",
            nodeId: no.id,
            aviso: true,
            texto: `${String(d.method ?? "GET").toUpperCase()} ${t(d.url) || "(sem URL)"} — não é chamada na simulação.`,
          }),
        );
        atual = proximoNo(g, no.id, "");
        break;
      }

      default: {
        estado.mensagens.push(
          msg({ autor: "sistema", nodeId: no.id, aviso: true, texto: `Tipo de nó "${no.type}" não roda em conversa.` }),
        );
        atual = proximoNo(g, no.id, "");
        break;
      }
    }
  }

  if (passos >= MAX_PASSOS) {
    estado.mensagens.push(
      msg({ autor: "sistema", aviso: true, texto: "Parei em 200 passos — o fluxo provavelmente tem um laço infinito." }),
    );
  }
  estado.esperandoEm = "";
  estado.esperandoTipo = "";
  estado.encerrado = true;
  return estado;
}

/** Começa uma conversa nova. */
export const iniciar = (g: FlowGraph, telefone?: string): SimEstado => {
  const estado = estadoInicial(telefone);
  const inicio = g.startNodeId || g.nodes[0]?.id || "";
  if (!inicio) {
    estado.mensagens.push(msg({ autor: "sistema", texto: "O fluxo não tem nenhum nó.", aviso: true }));
    estado.encerrado = true;
    return estado;
  }
  return executar(g, estado, inicio);
};

/**
 * Entrega a resposta do cliente ao nó que estava esperando.
 *
 * A regra de casamento imita `matchChatOptionBranch`: compara com a `key`, com
 * o rótulo e com a posição 1-based. Não casando nada, o executor usa o texto
 * cru como handle — o que na prática cai na aresta padrão.
 */
export const responder = (g: FlowGraph, estado: SimEstado, resposta: string): SimEstado => {
  if (!estado.esperandoEm) return estado;
  const no = acharNo(g, estado.esperandoEm);
  if (!no) return estado;

  const texto = resposta.trim();
  estado.mensagens.push(msg({ autor: "cliente", texto }));
  (estado.vars.message as Record<string, unknown>).body = texto;

  const d = (no.data ?? {}) as FlowNodeData;
  const saveAs = String(d.saveAs ?? "").trim();

  if (estado.esperandoTipo === "input") {
    setVar(estado, saveAs || `${no.id}_input`, texto);
    setVar(estado, `${no.id}_input`, texto);
    estado.esperandoEm = "";
    estado.esperandoTipo = "";
    return executar(g, estado, proximoNo(g, no.id, ""));
  }

  const opts = opcoesDe(d);
  const alvo = texto.toLowerCase();
  let handle = "";
  opts.forEach((o, i) => {
    if (handle) return;
    if (o.key.toLowerCase() === alvo || o.label.toLowerCase() === alvo || String(i + 1) === texto) {
      handle = o.key;
    }
  });

  if (!handle) {
    estado.mensagens.push(
      msg({
        autor: "sistema",
        nodeId: no.id,
        aviso: true,
        texto: "A resposta não casou com nenhuma opção — o executor segue pela saída padrão do nó.",
      }),
    );
  }
  if (saveAs) setVar(estado, saveAs, handle || texto);
  setVar(estado, `${no.id}_choice`, handle || texto);
  setVar(estado, `${no.id}_input`, texto);

  estado.esperandoEm = "";
  estado.esperandoTipo = "";
  return executar(g, estado, proximoNo(g, no.id, handle));
};

/** Saídas de um nó que não levam a lugar nenhum — vira aviso no editor. */
export const saidasSoltas = (g: FlowGraph): Array<{ nodeId: string; handle: string; label: string }> => {
  const soltas: Array<{ nodeId: string; handle: string; label: string }> = [];
  for (const n of g.nodes) {
    for (const s of outputsOf(n)) {
      const ligada = g.edges.some((e) => e.source === n.id && (e.sourceHandle ?? "") === s.handle);
      if (!ligada) soltas.push({ nodeId: n.id, handle: s.handle, label: s.label });
    }
  }
  return soltas;
};
