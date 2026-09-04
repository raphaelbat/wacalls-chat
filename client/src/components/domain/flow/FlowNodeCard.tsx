/**
 * O cartão de um nó no canvas.
 *
 * Cada saída vira um ponto de conexão próprio à direita, com rótulo — é assim
 * que "Sim/Não" e as opções de um menu ficam visíveis sem abrir o inspetor.
 */

import { memo } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import * as Icons from "lucide-react";
import type { FlowNodeData } from "@/types/flow";
import { outputsOf, specOf, type NodeGroup } from "./node-catalog";

const COR_DO_GRUPO: Record<NodeGroup, string> = {
  conteudo: "border-l-emerald-500",
  interacao: "border-l-sky-500",
  logica: "border-l-amber-500",
  sistema: "border-l-violet-500",
  integracoes: "border-l-rose-500",
};

const FUNDO_DO_GRUPO: Record<NodeGroup, string> = {
  conteudo: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
  interacao: "bg-sky-500/10 text-sky-600 dark:text-sky-400",
  logica: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  sistema: "bg-violet-500/10 text-violet-600 dark:text-violet-400",
  integracoes: "bg-rose-500/10 text-rose-600 dark:text-rose-400",
};

/** Uma linha curta com o que o nó faz, para dar de ler o fluxo sem clicar. */
const resumo = (type: string, data: FlowNodeData): string => {
  const txt = (v: unknown) => String(v ?? "").trim();
  switch (type) {
    case "chat_text":
    case "chat_content":
      return txt(data.mediaUrl) ? `${txt(data.mediaKind) || "mídia"}: ${txt(data.mediaUrl)}` : txt(data.text) || "sem texto";
    case "chat_menu":
    case "chat_msg_api":
    case "chat_input":
      return txt(data.prompt) || txt(data.text) || "sem pergunta";
    case "chat_interval":
      return `${data.seconds ?? 1}s`;
    case "chat_if_else": {
      const c = Array.isArray(data.conditions) ? data.conditions[0] : null;
      if (c) return `${txt(c.variable) || "?"} ${txt(c.operator) || "eq"} ${txt(c.value)}`;
      return `${txt(data.variable) || "?"} ${txt(data.operator) || "eq"} ${txt(data.value)}`;
    }
    case "chat_queue":
      return txt(data.queueId) ? `fila ${txt(data.queueId)}` : "sem fila";
    case "chat_tag_add":
    case "chat_tag_remove":
      return txt(data.tag) || "sem tag";
    case "chat_variable":
      return txt(data.variable) ? `${txt(data.variable)} = ${txt(data.value)}` : "sem variável";
    case "chat_http":
    case "chat_n8n":
      return `${txt(data.method) || "GET"} ${txt(data.url) || "sem URL"}`;
    case "chat_attendant":
      return txt(data.destination) || "sem destino";
    case "chat_ai_agent":
      return txt(data.agentId) || "sem agente";
    case "chat_switch_flow":
      return txt(data.flowId) || "sem fluxo";
    default:
      return "";
  }
};

export type DadosDoCartao = {
  data: FlowNodeData;
  ehInicio?: boolean;
  visitado?: boolean;
};

const FlowNodeCardBase = ({ id, type, data, selected }: NodeProps) => {
  const spec = specOf(type);
  const dados = (data as unknown as DadosDoCartao) ?? { data: {} };
  const conteudo = dados.data ?? {};
  const saidas = outputsOf({ type: type as never, data: conteudo });
  const grupo = spec?.group ?? "sistema";
  const Icone = (Icons as unknown as Record<string, React.ComponentType<{ className?: string }>>)[
    spec?.icon ?? "Circle"
  ] ?? Icons.Circle;

  const alturaExtra = Math.max(0, saidas.length - 1) * 22;

  return (
    <div
      className={`relative rounded-xl border border-l-4 bg-card shadow-sm transition ${COR_DO_GRUPO[grupo]} ${
        selected ? "ring-2 ring-primary" : ""
      } ${dados.visitado ? "outline outline-2 outline-offset-2 outline-primary/40" : ""}`}
      style={{ width: 248, paddingBottom: alturaExtra }}
    >
      <Handle
        type="target"
        position={Position.Left}
        className="!h-2.5 !w-2.5 !border-2 !border-background !bg-muted-foreground"
      />

      <div className="flex items-center gap-2 px-3 pt-2.5">
        <span className={`grid h-6 w-6 shrink-0 place-items-center rounded-md ${FUNDO_DO_GRUPO[grupo]}`}>
          <Icone className="h-3.5 w-3.5" />
        </span>
        <span className="truncate text-[13px] font-semibold">{spec?.label ?? type}</span>
        {dados.ehInicio ? (
          <span className="ml-auto rounded bg-primary/15 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-primary">
            início
          </span>
        ) : null}
      </div>

      <p className="line-clamp-2 px-3 pb-2.5 pt-1 text-[11px] leading-snug text-muted-foreground">
        {resumo(type, conteudo) || spec?.hint}
      </p>

      {spec?.placeholder ? (
        <div className="mx-3 mb-2.5 flex items-start gap-1.5 rounded-md bg-amber-500/10 px-2 py-1 text-[10px] leading-snug text-amber-700 dark:text-amber-400">
          <Icons.TriangleAlert className="mt-px h-3 w-3 shrink-0" />
          <span>Ainda não executa de verdade</span>
        </div>
      ) : null}

      {saidas.map((s, i) => (
        <div key={`${id}-${s.handle}-${i}`}>
          {s.label ? (
            <span
              className={`absolute right-4 text-[10px] font-medium ${
                s.tone === "sim"
                  ? "text-emerald-600 dark:text-emerald-400"
                  : s.tone === "nao"
                    ? "text-rose-600 dark:text-rose-400"
                    : "text-muted-foreground"
              }`}
              style={{ top: `calc(100% - ${alturaExtra + 14}px + ${i * 22}px)` }}
            >
              {s.label}
            </span>
          ) : null}
          <Handle
            id={s.handle || undefined}
            type="source"
            position={Position.Right}
            style={{ top: `calc(100% - ${alturaExtra + 8}px + ${i * 22}px)` }}
            className={`!h-2.5 !w-2.5 !border-2 !border-background ${
              s.tone === "sim" ? "!bg-emerald-500" : s.tone === "nao" ? "!bg-rose-500" : "!bg-primary"
            }`}
          />
        </div>
      ))}
    </div>
  );
};

export const FlowNodeCard = memo(FlowNodeCardBase);
