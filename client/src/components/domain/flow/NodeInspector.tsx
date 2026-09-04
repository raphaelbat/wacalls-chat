/**
 * Painel de propriedades do nó selecionado.
 *
 * Cada tipo mostra só os campos que o executor lê de verdade — os nomes das
 * chaves vêm do catálogo, e não há campo decorativo aqui: se aparece na tela,
 * o servidor usa.
 */

import { useEffect, useState } from "react";
import { Plus, Trash2, TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { listQueues } from "@/services/queues";
import type { Queue } from "@/types/queue";
import type { FlowNode, FlowNodeData, FlowRow } from "@/types/flow";
import { specOf } from "./node-catalog";

type Props = {
  node: FlowNode;
  fluxos: FlowRow[];
  ehInicio: boolean;
  onChange: (data: FlowNodeData) => void;
  onDefinirInicio: () => void;
  onRemover: () => void;
};

const selectClass = "h-10 w-full rounded-md border bg-background px-3 text-sm";

const Campo = ({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) => (
  <div className="space-y-1.5">
    <Label className="text-xs font-medium">{label}</Label>
    {children}
    {hint ? <p className="text-[11px] leading-snug text-muted-foreground">{hint}</p> : null}
  </div>
);

/** Lista de opções — vira uma saída por linha no canvas. */
const EditorDeOpcoes = ({
  opcoes,
  comPeso,
  onChange,
}: {
  opcoes: Array<Record<string, unknown>>;
  comPeso?: boolean;
  onChange: (v: Array<Record<string, unknown>>) => void;
}) => {
  const alterar = (i: number, campo: string, valor: unknown) => {
    const copia = opcoes.map((o, idx) => (idx === i ? { ...o, [campo]: valor } : o));
    onChange(copia);
  };
  return (
    <div className="space-y-2">
      {opcoes.map((o, i) => (
        <div key={i} className="flex items-center gap-1.5">
          <Input
            value={String(o.key ?? "")}
            onChange={(e) => alterar(i, "key", e.target.value)}
            placeholder="id"
            className="h-9 w-20 font-mono text-xs"
          />
          <Input
            value={String(o.label ?? "")}
            onChange={(e) => alterar(i, "label", e.target.value)}
            placeholder="O que o cliente vê"
            className="h-9 flex-1 text-sm"
          />
          {comPeso ? (
            <Input
              type="number"
              min={0}
              value={String(o.weight ?? 1)}
              onChange={(e) => alterar(i, "weight", Number(e.target.value))}
              className="h-9 w-16 text-sm"
              title="Peso no sorteio"
            />
          ) : null}
          <Button
            variant="ghost"
            size="icon"
            className="h-9 w-9 shrink-0 text-muted-foreground hover:text-destructive"
            onClick={() => onChange(opcoes.filter((_, idx) => idx !== i))}
            aria-label="Remover opção"
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      ))}
      <Button
        variant="outline"
        size="sm"
        className="w-full"
        onClick={() => onChange([...opcoes, { key: "", label: "", ...(comPeso ? { weight: 1 } : {}) }])}
      >
        <Plus className="mr-1.5 h-3.5 w-3.5" />
        Adicionar opção
      </Button>
      <p className="text-[11px] leading-snug text-muted-foreground">
        Cada opção vira uma saída do nó — ligue cada uma ao próximo passo. O <strong>id</strong> é o que
        identifica a escolha; deixando em branco, vale a posição (1, 2, 3…).
      </p>
    </div>
  );
};

const OPERADORES: Array<{ v: string; t: string }> = [
  { v: "eq", t: "é igual a" },
  { v: "neq", t: "é diferente de" },
  { v: "contains", t: "contém" },
  { v: "starts_with", t: "começa com" },
  { v: "empty", t: "está vazio" },
  { v: "not_empty", t: "não está vazio" },
];

export const NodeInspector = ({ node, fluxos, ehInicio, onChange, onDefinirInicio, onRemover }: Props) => {
  const [filas, setFilas] = useState<Queue[]>([]);
  const spec = specOf(node.type);
  const d = node.data ?? {};
  const set = (campo: string, valor: unknown) => onChange({ ...d, [campo]: valor });
  const str = (v: unknown) => String(v ?? "");

  useEffect(() => {
    if (node.type === "chat_queue") void listQueues().then(setFilas).catch(() => {});
  }, [node.type]);

  const opcoes = (Array.isArray(d.options) ? d.options : []) as Array<Record<string, unknown>>;
  const condicoes = (Array.isArray(d.conditions) ? d.conditions : []) as Array<Record<string, unknown>>;

  return (
    <div className="flex h-full flex-col">
      <div className="border-b px-4 py-3">
        <div className="text-sm font-semibold">{spec?.label ?? node.type}</div>
        <p className="mt-0.5 text-[11px] leading-snug text-muted-foreground">{spec?.hint}</p>
      </div>

      <div className="flex-1 space-y-4 overflow-y-auto px-4 py-4">
        {spec?.placeholder ? (
          <div className="flex items-start gap-2 rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2.5 text-[11px] leading-snug text-amber-700 dark:text-amber-400">
            <TriangleAlert className="mt-px h-3.5 w-3.5 shrink-0" />
            <span>{spec.placeholder}</span>
          </div>
        ) : null}

        {/* ---------------------------------------------------------- texto */}
        {(node.type === "chat_text" || node.type === "chat_content") && (
          <>
            <Campo
              label="Mensagem"
              hint="Use {{.vars.nome}} para inserir uma variável, ou {{.message.body}} para a última mensagem do cliente."
            >
              <Textarea rows={4} value={str(d.text)} onChange={(e) => set("text", e.target.value)} />
            </Campo>
            {node.type === "chat_content" && (
              <>
                <Campo label="Endereço do arquivo" hint="URL pública da imagem, vídeo, áudio ou documento.">
                  <Input value={str(d.mediaUrl)} onChange={(e) => set("mediaUrl", e.target.value)} placeholder="https://…" />
                </Campo>
                <Campo label="Tipo">
                  <select className={selectClass} value={str(d.mediaKind) || "image"} onChange={(e) => set("mediaKind", e.target.value)}>
                    <option value="image">Imagem</option>
                    <option value="video">Vídeo</option>
                    <option value="audio">Áudio</option>
                    <option value="document">Documento</option>
                  </select>
                </Campo>
                <Campo label="Nome do arquivo" hint="Só para documento — é o nome que o cliente vê.">
                  <Input value={str(d.filename)} onChange={(e) => set("filename", e.target.value)} />
                </Campo>
              </>
            )}
          </>
        )}

        {/* ------------------------------------------------------- perguntas */}
        {(node.type === "chat_menu" || node.type === "chat_msg_api" || node.type === "chat_input") && (
          <>
            <Campo label="Pergunta">
              <Textarea rows={3} value={str(d.prompt)} onChange={(e) => set("prompt", e.target.value)} />
            </Campo>

            {node.type !== "chat_input" && (
              <>
                <Campo label="Rodapé" hint="Linha pequena embaixo da mensagem. Opcional.">
                  <Input value={str(d.footer)} onChange={(e) => set("footer", e.target.value)} />
                </Campo>
                <Campo
                  label="Formato"
                  hint={
                    node.type === "chat_msg_api"
                      ? "Botões valem até 3 opções; acima disso o WhatsApp exige lista. Se o envio nativo falhar, o servidor manda um menu numerado."
                      : "O menu numerado funciona em qualquer aparelho e nunca falha."
                  }
                >
                  <select className={selectClass} value={str(d.renderAs) || "text"} onChange={(e) => set("renderAs", e.target.value)}>
                    <option value="text">Menu numerado (texto)</option>
                    <option value="buttons">Botões nativos</option>
                    <option value="list">Lista nativa</option>
                  </select>
                </Campo>
                {str(d.renderAs) === "list" && (
                  <Campo label="Texto do botão da lista">
                    <Input value={str(d.buttonText)} onChange={(e) => set("buttonText", e.target.value)} placeholder="Ver opções" />
                  </Campo>
                )}
                <Campo label="Opções">
                  <EditorDeOpcoes opcoes={opcoes} onChange={(v) => set("options", v)} />
                </Campo>
              </>
            )}

            <Campo
              label="Guardar resposta em"
              hint={`Depois você usa como {{.vars.${str(d.saveAs) || "resposta"}}}. Em branco, o servidor guarda em ${node.id}_input.`}
            >
              <Input value={str(d.saveAs)} onChange={(e) => set("saveAs", e.target.value)} placeholder="resposta" className="font-mono text-sm" />
            </Campo>
          </>
        )}

        {/* -------------------------------------------------------- intervalo */}
        {node.type === "chat_interval" && (
          <Campo label="Segundos" hint="Dá um respiro entre mensagens, para não parecer robô disparando.">
            <Input type="number" min={1} value={String(d.seconds ?? 1)} onChange={(e) => set("seconds", Number(e.target.value))} />
          </Campo>
        )}

        {/* ------------------------------------------------------- se / senão */}
        {node.type === "chat_if_else" && (
          <>
            <Campo label="Combinar condições com">
              <select className={selectClass} value={str(d.logic) || "and"} onChange={(e) => set("logic", e.target.value)}>
                <option value="and">E — todas precisam ser verdadeiras</option>
                <option value="or">OU — basta uma ser verdadeira</option>
              </select>
            </Campo>
            <Campo
              label="Condições"
              hint="No campo da variável use o caminho completo: message.body para o texto do cliente, ou vars.nome para algo que você guardou."
            >
              <div className="space-y-2">
                {condicoes.map((c, i) => (
                  <div key={i} className="space-y-1.5 rounded-lg border bg-muted/20 p-2">
                    <div className="flex items-center gap-1.5">
                      <Input
                        value={str(c.variable)}
                        onChange={(e) =>
                          set("conditions", condicoes.map((x, idx) => (idx === i ? { ...x, variable: e.target.value } : x)))
                        }
                        placeholder="message.body"
                        className="h-9 flex-1 font-mono text-xs"
                      />
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-9 w-9 shrink-0 text-muted-foreground hover:text-destructive"
                        onClick={() => set("conditions", condicoes.filter((_, idx) => idx !== i))}
                        aria-label="Remover condição"
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                    <div className="flex items-center gap-1.5">
                      <select
                        className="h-9 w-40 rounded-md border bg-background px-2 text-xs"
                        value={str(c.operator) || "eq"}
                        onChange={(e) =>
                          set("conditions", condicoes.map((x, idx) => (idx === i ? { ...x, operator: e.target.value } : x)))
                        }
                      >
                        {OPERADORES.map((o) => (
                          <option key={o.v} value={o.v}>
                            {o.t}
                          </option>
                        ))}
                      </select>
                      {!["empty", "not_empty"].includes(str(c.operator) || "eq") && (
                        <Input
                          value={str(c.value)}
                          onChange={(e) =>
                            set("conditions", condicoes.map((x, idx) => (idx === i ? { ...x, value: e.target.value } : x)))
                          }
                          placeholder="valor"
                          className="h-9 flex-1 text-sm"
                        />
                      )}
                    </div>
                  </div>
                ))}
                <Button
                  variant="outline"
                  size="sm"
                  className="w-full"
                  onClick={() => set("conditions", [...condicoes, { variable: "", operator: "eq", value: "" }])}
                >
                  <Plus className="mr-1.5 h-3.5 w-3.5" />
                  Adicionar condição
                </Button>
              </div>
            </Campo>
          </>
        )}

        {/* ----------------------------------------------------- randomizador */}
        {node.type === "chat_random" && (
          <Campo label="Caminhos" hint="O peso é relativo: 70 e 30 dá 70% e 30%.">
            <EditorDeOpcoes opcoes={opcoes} comPeso onChange={(v) => set("options", v)} />
          </Campo>
        )}

        {/* ------------------------------------------------------------ fila */}
        {node.type === "chat_queue" && (
          <Campo label="Fila" hint="A conversa passa a contar para esta fila.">
            <select className={selectClass} value={str(d.queueId)} onChange={(e) => set("queueId", e.target.value)}>
              <option value="">— Escolha —</option>
              {filas.map((f) => (
                <option key={f.id} value={f.id}>
                  {f.name}
                </option>
              ))}
            </select>
          </Campo>
        )}

        {/* ------------------------------------------------------- atendente */}
        {node.type === "chat_attendant" && (
          <Campo label="Destino" hint="Nome ou identificador de quem assume. Registrado no histórico do fluxo.">
            <Input value={str(d.destination)} onChange={(e) => set("destination", e.target.value)} />
          </Campo>
        )}

        {/* ------------------------------------------------------------ tags */}
        {(node.type === "chat_tag_add" || node.type === "chat_tag_remove") && (
          <Campo label="Tag">
            <Input value={str(d.tag)} onChange={(e) => set("tag", e.target.value)} placeholder="cliente-novo" />
          </Campo>
        )}

        {/* ------------------------------------------------------ trocar fluxo */}
        {node.type === "chat_switch_flow" && (
          <Campo label="Fluxo de destino">
            <select className={selectClass} value={str(d.flowId)} onChange={(e) => set("flowId", e.target.value)}>
              <option value="">— Escolha —</option>
              {fluxos.map((f) => (
                <option key={f.id} value={f.id}>
                  {f.name}
                </option>
              ))}
            </select>
          </Campo>
        )}

        {/* -------------------------------------------------------- variável */}
        {node.type === "chat_variable" && (
          <>
            <Campo label="Nome">
              <Input value={str(d.variable)} onChange={(e) => set("variable", e.target.value)} placeholder="cliente" className="font-mono text-sm" />
            </Campo>
            <Campo label="Valor" hint="Aceita variáveis: {{.message.body}} guarda o que o cliente acabou de escrever.">
              <Input value={str(d.value)} onChange={(e) => set("value", e.target.value)} />
            </Campo>
          </>
        )}

        {/* ------------------------------------------------------------ HTTP */}
        {(node.type === "chat_http" || node.type === "chat_n8n") && (
          <>
            <Campo label="Método">
              <select className={selectClass} value={str(d.method) || (node.type === "chat_n8n" ? "POST" : "GET")} onChange={(e) => set("method", e.target.value)}>
                {["GET", "POST", "PUT", "DELETE"].map((m) => (
                  <option key={m} value={m}>
                    {m}
                  </option>
                ))}
              </select>
            </Campo>
            <Campo label="Endereço">
              <Input value={str(d.url)} onChange={(e) => set("url", e.target.value)} placeholder="https://…" />
            </Campo>
            <Campo label="Corpo" hint="JSON enviado na requisição. Aceita variáveis.">
              <Textarea rows={3} value={str(d.body)} onChange={(e) => set("body", e.target.value)} className="font-mono text-xs" />
            </Campo>
            {node.type === "chat_http" ? (
              <>
                <Campo label="Cabeçalhos" hint='Um JSON só, por exemplo {"Authorization":"Bearer abc"}.'>
                  <Textarea rows={2} value={str(d.headers)} onChange={(e) => set("headers", e.target.value)} className="font-mono text-xs" />
                </Campo>
                <Campo label="Guardar resposta em">
                  <Input value={str(d.saveAs)} onChange={(e) => set("saveAs", e.target.value)} placeholder="apiResponse" className="font-mono text-sm" />
                </Campo>
              </>
            ) : (
              <>
                <Campo label="Cabeçalho de autenticação">
                  <Input value={str(d.authHeader)} onChange={(e) => set("authHeader", e.target.value)} />
                </Campo>
                <Campo label="Guardar resposta em">
                  <Input value={str(d.responseVariable)} onChange={(e) => set("responseVariable", e.target.value)} className="font-mono text-sm" />
                </Campo>
              </>
            )}
          </>
        )}

        {/* -------------------------------------------------------- agente IA */}
        {node.type === "chat_ai_agent" && (
          <Campo label="Identificador do agente">
            <Input value={str(d.agentId)} onChange={(e) => set("agentId", e.target.value)} />
          </Campo>
        )}
      </div>

      <div className="flex items-center gap-2 border-t bg-muted/20 px-4 py-3">
        <Button variant="outline" size="sm" className="flex-1" onClick={onDefinirInicio} disabled={ehInicio}>
          {ehInicio ? "É o primeiro passo" : "Começar por aqui"}
        </Button>
        <Button variant="ghost" size="icon" onClick={onRemover} className="text-muted-foreground hover:text-destructive" aria-label="Remover nó">
          <Trash2 className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
};
