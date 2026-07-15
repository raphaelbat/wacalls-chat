import { apiGet, apiPost, apiDelete, apiPatch } from "@/lib/api";

export type CartGateway = "kiwify" | "hotmart" | "cakto" | "braip" | "generic";

export interface CartIntegration {
  id: string;
  ownerUser: string;
  gateway: CartGateway | string;
  name: string;
  webhookSecret: string;
  active: boolean;
  sessionId?: string;
  queueId?: string;
  flowId?: string;
  cadenceId?: string;
  fromName?: string;
  createdAt: number;
}

export type CartChannel = "whatsapp" | "call" | "email";

export interface CartCadenceStep {
  channel: CartChannel;
  delayMinutes: number;
  template?: string;
  mediaUrl?: string;
  subject?: string;
  callMode?: "ura" | "queue";
  flowId?: string;
  queueId?: string;
}

export interface CartCadence {
  id: string;
  ownerUser: string;
  name: string;
  steps: CartCadenceStep[];
  createdAt: number;
  updatedAt: number;
}

export interface CartRow {
  id: string;
  integrationId: string;
  ownerUser: string;
  gateway: string;
  externalId: string;
  buyerName: string;
  buyerPhone: string;
  buyerEmail: string;
  productName: string;
  amountCents: number;
  currency: string;
  checkoutUrl: string;
  status: "pending" | "recovered" | "lost" | "converted";
  receivedAt: number;
  recoveredAt?: number;
}

export interface CartAttempt {
  id: string;
  cartId: string;
  stepIndex: number;
  channel: CartChannel;
  scheduledAt: number;
  sentAt?: number;
  status: "queued" | "sent" | "failed" | "skipped";
  callId?: string;
  messageId?: string;
  error?: string;
}

export interface CartStats {
  window: string;
  total: number;
  pending: number;
  recovered: number;
  converted: number;
  lost: number;
  revenueCents: number;
  recoveryRate: number;
}

export interface CartWebhookLog {
  id: string;
  integrationId?: string;
  ownerUser?: string;
  gateway?: string;
  event?: string;
  status: "ok" | "converted" | "ignored" | "failed" | string;
  httpStatus: number;
  externalId?: string;
  cartId?: string;
  error?: string;
  payload?: unknown;
  remoteAddr?: string;
  receivedAt: number;
  replayedFrom?: string;
}

export interface CartReplayResult {
  status: string;
  httpStatus: number;
  cartId?: string;
  event?: string;
  error?: string;
  reason?: string;
}

export const cartRecovery = {
  stats: () => apiGet<CartStats>("/api/cart/stats"),
  listIntegrations: () => apiGet<{ items: CartIntegration[] }>("/api/cart/integrations"),
  createIntegration: (input: Partial<CartIntegration>) =>
    apiPost<CartIntegration>("/api/cart/integrations", input),
  updateIntegration: (id: string, input: Partial<CartIntegration>) =>
    apiPatch<{ status: string }>(`/api/cart/integrations/${id}`, input),
  deleteIntegration: (id: string) => apiDelete(`/api/cart/integrations/${id}`),

  listCadences: () => apiGet<{ items: CartCadence[] }>("/api/cart/cadences"),
  saveCadence: (input: Partial<CartCadence>) => {
    if (input.id) return apiPatch<CartCadence>(`/api/cart/cadences/${input.id}`, input);
    return apiPost<CartCadence>("/api/cart/cadences", input);
  },
  deleteCadence: (id: string) => apiDelete(`/api/cart/cadences/${id}`),

  listCarts: (status?: string) =>
    apiGet<{ items: CartRow[] }>(`/api/cart/carts${status ? `?status=${status}` : ""}`),
  getCart: (id: string) => apiGet<{ cart: CartRow; attempts: CartAttempt[] }>(`/api/cart/carts/${id}`),
  markLost: (id: string) => apiPost<{ status: string }>(`/api/cart/carts/${id}/lost`, {}),

  listWebhookLogs: (params: { status?: string; integrationId?: string; limit?: number } = {}) => {
    const q = new URLSearchParams();
    if (params.status) q.set("status", params.status);
    if (params.integrationId) q.set("integrationId", params.integrationId);
    if (params.limit) q.set("limit", String(params.limit));
    const qs = q.toString();
    return apiGet<{ items: CartWebhookLog[] }>(`/api/cart/webhooks${qs ? `?${qs}` : ""}`);
  },
  webhookCounts: () => apiGet<Record<string, number>>("/api/cart/webhooks/counts"),
  getWebhookLog: (id: string) => apiGet<CartWebhookLog>(`/api/cart/webhooks/${id}`),
  replayWebhook: (id: string) => apiPost<CartReplayResult>(`/api/cart/webhooks/${id}/replay`, {}),
};
