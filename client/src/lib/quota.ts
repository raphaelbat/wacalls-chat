import { toast } from "sonner";
import { usePlanStore } from "@/stores/plan";

type QuotaKind = "usuarios" | "conexoes" | "filas";

const LABEL: Record<QuotaKind, string> = {
  usuarios: "usuários",
  conexoes: "conexões",
  filas: "filas",
};

/** Payload estruturado devolvido pelo backend em respostas 402 (writeQuotaError). */
export type QuotaPayload = {
  code?: string;
  kind?: QuotaKind | string;
  label?: string;
  limit?: number;
  current?: number;
  upgrade?: boolean;
  error?: string;
};

function formatQuotaMessage(kind: string, label: string, limit: number, current: number) {
  const niceLabel = label || LABEL[kind as QuotaKind] || kind;
  const usage = Number.isFinite(current) ? ` (uso atual: ${current})` : "";
  return `Limite do plano atingido: ${limit} ${niceLabel} permitidas${usage}. Faça upgrade do plano para liberar mais.`;
}

let lastQuotaToastAt = 0;
function emitQuotaToast(message: string) {
  const now = Date.now();
  // Evita toasts duplicados quando múltiplas chamadas estouram em sequência.
  if (now - lastQuotaToastAt < 1500) return;
  lastQuotaToastAt = now;
  toast.error(message, {
    description: "Atualize seu plano em Configurações → Planos para liberar mais.",
    duration: 6000,
  });
}

/**
 * Trata um payload 402 vindo da API. Retorna true se reconheceu como cota de
 * plano e exibiu o toast padronizado, false caso contrário (para o chamador
 * decidir o fallback).
 */
export function handleQuotaResponse(payload: QuotaPayload | null | undefined): boolean {
  if (!payload || typeof payload !== "object") return false;
  if (payload.code !== "quota_exceeded" && !payload.kind) return false;
  const kind = String(payload.kind ?? "");
  if (kind === "payment_required") {
    emitQuotaToast(payload.error || "Para conectar um WhatsApp é necessário um plano ativo.");
    return true;
  }
  const label = String(payload.label ?? LABEL[kind as QuotaKind] ?? kind);
  const limit = Number(payload.limit ?? 0);
  const current = Number(payload.current ?? 0);
  const msg =
    limit > 0
      ? formatQuotaMessage(kind, label, limit, current)
      : payload.error || "Limite do plano atingido. Faça upgrade do plano para liberar mais.";
  emitQuotaToast(msg);
  return true;
}

/**
 * Pré-checa cota do plano ativo no frontend antes de disparar a ação.
 * Retorna true se a operação pode prosseguir, false (e mostra toast) se
 * o limite seria estourado. O backend faz a checagem definitiva (HTTP 402),
 * isto aqui apenas evita ida desnecessária ao servidor e melhora UX.
 */
export function ensureQuota(kind: QuotaKind, current: number): boolean {
  const plan = usePlanStore.getState().plan;
  if (!plan) return true; // sem plano ativo = ilimitado
  const raw = Number(plan[kind] ?? 0);
  if (raw <= 0) return true; // 0 = ilimitado
  if (current >= raw) {
    emitQuotaToast(formatQuotaMessage(kind, LABEL[kind], raw, current));
    return false;
  }
  return true;
}