import type { CartCadenceStep } from "@/services/cartRecovery";

/** Valores de exemplo usados nas pré-visualizações. */
export const SAMPLE_VARS: Record<string, string> = {
  name: "Maria Silva",
  product: "Curso Tráfego Pago Pro",
  checkout: "https://pay.minhaloja.com/c/AB12CD",
  amount: "R$ 297,00",
  email: "maria.silva@email.com",
  phone: "+55 11 99876-5432",
  store: "Minha Loja Digital",
  customer_phone: "+55 11 99876-5432",
  payment_status: "pendente",
  discount_code: "VOLTA10",
  pix_code: "00020126360014BR.GOV.BCB.PIX...EXEMPLO",
  due_date: "amanhã",
};

/** Lista completa de variáveis aceitas (exibida no editor). */
export const AVAILABLE_VARIABLES = [
  "name",
  "product",
  "checkout",
  "amount",
  "email",
  "phone",
  "store",
  "customer_phone",
  "payment_status",
  "discount_code",
  "pix_code",
  "due_date",
] as const;

export interface RenderResult {
  rendered: string;
  missing: string[]; // placeholders sem valor
  unknown: string[]; // placeholders sem variável correspondente em SAMPLE_VARS
}

/** Substitui {{var}} pelos valores de `vars` e retorna info de validação. */
export function renderTemplate(
  template: string,
  vars: Record<string, string> = SAMPLE_VARS,
): RenderResult {
  const missing = new Set<string>();
  const unknown = new Set<string>();
  const rendered = (template || "").replace(/\{\{\s*([a-zA-Z0-9_]+)\s*\}\}/g, (_m, key) => {
    if (!(key in vars)) {
      unknown.add(key);
      return `{{${key}}}`;
    }
    const v = vars[key];
    if (!v || !String(v).trim()) {
      missing.add(key);
      return `{{${key}}}`;
    }
    return String(v);
  });
  return { rendered, missing: [...missing], unknown: [...unknown] };
}

/** Soma `delayMinutes` ao tempo base e formata em pt-BR. */
export function scheduleAt(baseMs: number, delayMinutes: number) {
  const d = new Date(baseMs + delayMinutes * 60_000);
  return d.toLocaleString("pt-BR", {
    day: "2-digit",
    month: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function humanDelay(min: number) {
  if (min <= 0) return "imediato";
  if (min < 60) return `${min} min`;
  if (min < 1440) {
    const h = Math.floor(min / 60);
    const r = min % 60;
    return r ? `${h}h${r}` : `${h}h`;
  }
  const d = Math.floor(min / 1440);
  const r = Math.floor((min % 1440) / 60);
  return r ? `${d}d${r}h` : `${d}d`;
}

export interface ScenarioMeta {
  label: string;
  /** Texto curto explicando o disparo. */
  trigger: string;
  /** Quando o "carrinho/PIX/etc." expira a partir do evento. Null = não expira. */
  expiresInMinutes: number | null;
}

/** Heurística para deduzir o cenário a partir do nome da régua. */
export function detectScenario(name: string): ScenarioMeta {
  const n = (name || "").toLowerCase();
  if (n.includes("pix")) {
    return {
      label: "PIX pendente",
      trigger: "Cliente gerou o PIX e ainda não pagou",
      expiresInMinutes: 30,
    };
  }
  if (n.includes("boleto")) {
    return {
      label: "Boleto pendente",
      trigger: "Boleto gerado, aguardando pagamento",
      expiresInMinutes: 3 * 1440,
    };
  }
  if (n.includes("aprovad") || n.includes("welcome") || n.includes("boas-vindas")) {
    return {
      label: "Pagamento aprovado",
      trigger: "Pagamento confirmado pelo gateway",
      expiresInMinutes: null,
    };
  }
  if (n.includes("upsell")) {
    return {
      label: "Upsell pós-venda",
      trigger: "Cliente comprou — oferta complementar",
      expiresInMinutes: null,
    };
  }
  if (n.includes("chargeback") || n.includes("pós-venda") || n.includes("pos-venda")) {
    return {
      label: "Pós-venda",
      trigger: "Compra concluída — relacionamento",
      expiresInMinutes: null,
    };
  }
  return {
    label: "Carrinho abandonado",
    trigger: "Cliente saiu do checkout sem pagar",
    expiresInMinutes: 24 * 60,
  };
}

export interface StepPreview {
  index: number;
  step: CartCadenceStep;
  scheduledLabel: string;
  delayLabel: string;
  channelLabel: string;
  subjectPreview?: RenderResult;
  bodyPreview: RenderResult;
}

export function buildPreview(
  steps: CartCadenceStep[],
  baseMs = Date.now(),
  vars: Record<string, string> = SAMPLE_VARS,
): StepPreview[] {
  return (steps || []).map((step, i) => {
    const channelLabel =
      step.channel === "whatsapp" ? "WhatsApp" : step.channel === "call" ? "Ligação VoIP" : "E-mail";
    const body = step.channel === "call" ? "(chamada automática — sem texto)" : step.template || "";
    return {
      index: i,
      step,
      scheduledLabel: scheduleAt(baseMs, step.delayMinutes || 0),
      delayLabel: humanDelay(step.delayMinutes || 0),
      channelLabel,
      subjectPreview: step.subject ? renderTemplate(step.subject, vars) : undefined,
      bodyPreview: renderTemplate(body, vars),
    };
  });
}
