/**
 * Simulador de conversa.
 *
 * Roda o fluxo aqui no navegador para você ver o caminho antes de ligar num
 * número de verdade. Nada é enviado e nenhuma API é chamada — os passos que
 * dependem do mundo real aparecem marcados.
 */

import { useEffect, useRef, useState } from "react";
import { Play, RotateCcw, Send, TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { FlowGraph } from "@/types/flow";
import { iniciar, responder, type SimEstado } from "./simulator";

type Props = {
  graph: FlowGraph;
  /** Destaca no canvas o nó que acabou de rodar. */
  onNoAtivo?: (nodeId: string) => void;
};

export const FlowSimulator = ({ graph, onNoAtivo }: Props) => {
  const [estado, setEstado] = useState<SimEstado | null>(null);
  const [texto, setTexto] = useState("");
  const fim = useRef<HTMLDivElement>(null);

  useEffect(() => {
    fim.current?.scrollIntoView({ behavior: "smooth", block: "end" });
    const ultimo = estado?.mensagens.filter((m) => m.nodeId).at(-1);
    if (ultimo?.nodeId) onNoAtivo?.(ultimo.nodeId);
  }, [estado, onNoAtivo]);

  const comecar = () => setEstado(iniciar(structuredClone(graph)));

  const enviar = (valor: string) => {
    const v = valor.trim();
    if (!v || !estado || !estado.esperandoEm) return;
    setEstado(responder(structuredClone(graph), structuredClone(estado), v));
    setTexto("");
  };

  const aguardando = !!estado?.esperandoEm;

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between gap-2 border-b px-4 py-3">
        <div>
          <div className="text-sm font-semibold">Simulador</div>
          <p className="text-[11px] text-muted-foreground">Roda no navegador, sem enviar mensagem</p>
        </div>
        <Button variant="outline" size="sm" onClick={comecar}>
          {estado ? <RotateCcw className="mr-1.5 h-3.5 w-3.5" /> : <Play className="mr-1.5 h-3.5 w-3.5" />}
          {estado ? "Recomeçar" : "Iniciar"}
        </Button>
      </div>

      <div className="flex-1 space-y-2 overflow-y-auto bg-muted/20 px-3 py-3">
        {!estado ? (
          <p className="px-1 py-8 text-center text-xs text-muted-foreground">
            Clique em Iniciar para percorrer o fluxo como se você fosse o cliente.
          </p>
        ) : null}

        {estado?.mensagens.map((m) =>
          m.autor === "sistema" ? (
            <div
              key={m.id}
              className={`mx-auto flex max-w-[95%] items-start gap-1.5 rounded-md px-2.5 py-1.5 text-[11px] leading-snug ${
                m.aviso
                  ? "bg-amber-500/10 text-amber-700 dark:text-amber-400"
                  : "bg-background/70 text-muted-foreground"
              }`}
            >
              {m.aviso ? <TriangleAlert className="mt-px h-3 w-3 shrink-0" /> : null}
              <span>{m.texto}</span>
            </div>
          ) : (
            <div key={m.id} className={`flex ${m.autor === "cliente" ? "justify-end" : "justify-start"}`}>
              <div
                className={`max-w-[85%] rounded-2xl px-3 py-2 text-[13px] leading-snug shadow-sm ${
                  m.autor === "cliente"
                    ? "rounded-br-sm bg-primary text-primary-foreground"
                    : "rounded-bl-sm bg-card"
                }`}
              >
                <span className="whitespace-pre-wrap">{m.texto}</span>
                {m.botoes?.length ? (
                  <div className="mt-2 flex flex-wrap gap-1.5 border-t pt-2">
                    {m.botoes.map((b) => (
                      <button
                        key={b.id}
                        type="button"
                        disabled={!aguardando}
                        onClick={() => enviar(b.label)}
                        className="rounded-full border border-primary/40 px-2.5 py-1 text-[11px] font-medium text-primary transition hover:bg-primary/10 disabled:opacity-40"
                      >
                        {b.label}
                      </button>
                    ))}
                  </div>
                ) : null}
              </div>
            </div>
          ),
        )}

        {estado?.encerrado ? (
          <p className="pt-2 text-center text-[11px] text-muted-foreground">Fim do fluxo.</p>
        ) : null}
        <div ref={fim} />
      </div>

      <form
        className="flex items-center gap-2 border-t px-3 py-2.5"
        onSubmit={(e) => {
          e.preventDefault();
          enviar(texto);
        }}
      >
        <Input
          value={texto}
          onChange={(e) => setTexto(e.target.value)}
          placeholder={aguardando ? "Responda como o cliente…" : "O fluxo não está esperando resposta"}
          disabled={!aguardando}
          className="h-9 text-sm"
        />
        <Button type="submit" size="icon" className="h-9 w-9 shrink-0" disabled={!aguardando || !texto.trim()} aria-label="Enviar">
          <Send className="h-4 w-4" />
        </Button>
      </form>

      {estado && Object.keys(estado.vars.vars as object).length > 0 ? (
        <div className="border-t px-3 py-2">
          <div className="mb-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">Variáveis</div>
          <div className="max-h-24 space-y-0.5 overflow-y-auto font-mono text-[10px]">
            {Object.entries(estado.vars.vars as Record<string, unknown>).map(([k, v]) => (
              <div key={k} className="flex gap-1.5">
                <span className="text-muted-foreground">{k}</span>
                <span className="truncate">{typeof v === "object" ? JSON.stringify(v) : String(v)}</span>
              </div>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
};
