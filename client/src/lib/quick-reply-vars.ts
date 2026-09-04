// Variáveis suportadas nos snippets de resposta rápida. O conteúdo do
// snippet usa a forma {{nome}} e é resolvido no momento da inserção.
export type QuickReplyContext = {
  contactName?: string;
  phone?: string;
  protocol?: string;
  agentName?: string;
  queueName?: string;
};

export const QUICK_REPLY_VARIABLES = [
  "nome",
  "primeiro_nome",
  "telefone",
  "protocolo",
  "atendente",
  "fila",
  "data",
  "hora",
  "saudacao",
] as const;

const greeting = (d: Date): string => {
  const h = d.getHours();
  if (h < 12) return "Bom dia";
  if (h < 18) return "Boa tarde";
  return "Boa noite";
};

export const resolveQuickReplyVars = (content: string, ctx: QuickReplyContext = {}): string => {
  const now = new Date();
  const name = (ctx.contactName ?? "").trim();
  const map: Record<string, string> = {
    nome: name,
    primeiro_nome: name.split(/\s+/)[0] ?? "",
    telefone: ctx.phone ?? "",
    protocolo: ctx.protocol ?? "",
    atendente: ctx.agentName ?? "",
    fila: ctx.queueName ?? "",
    data: now.toLocaleDateString("pt-BR"),
    hora: now.toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" }),
    saudacao: greeting(now),
  };
  return content.replace(/\{\{\s*([a-z_]+)\s*\}\}/gi, (full, key: string) => {
    const v = map[key.toLowerCase()];
    return v === undefined ? full : v;
  });
};