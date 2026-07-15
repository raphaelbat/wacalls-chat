import { apiUrl } from "@/lib/api-base";

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(apiUrl(path), {
    credentials: "include",
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
    ...init,
  });
  if (!res.ok) {
    let msg = `${res.status}`;
    try {
      const j = await res.json();
      if (j?.error) msg = typeof j.error === "string" ? j.error : JSON.stringify(j.error);
      if (j?.detail) {
        const d = j.detail;
        const stripeMsg = d?.error?.message || d?.message;
        if (stripeMsg) msg = `Stripe: ${stripeMsg}`;
      }
    } catch {
      /* ignore */
    }
    throw new Error(msg);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

export type BillingConfig = {
  secretKeyMask: string;
  webhookSecretSet: boolean;
  publishableKey: string;
  currency: string;
  enabled: boolean;
  requirePaidToConnect?: boolean;
};

export const getBillingConfig = () => req<BillingConfig>("/api/billing/config");

export const saveBillingConfig = (
  partial: Partial<{
    secretKey: string;
    webhookSecret: string;
    publishableKey: string;
    currency: string;
    requirePaidToConnect: boolean;
  }>,
) =>
  req<BillingConfig>("/api/billing/config", {
    method: "PUT",
    body: JSON.stringify(partial),
  });

export type Subscription = {
  userId: string;
  planId: string;
  status: string;
  stripeCustomer?: string;
  stripeSubscription?: string;
  quantity: number;
  currentPeriodEnd: number;
  updatedAt: number;
};

export const getSubscription = () => req<Subscription>("/api/billing/subscription");

export const setSubscriptionPlan = (planId: string) =>
  req<Subscription>("/api/billing/subscription/plan", {
    method: "PUT",
    body: JSON.stringify({ planId }),
  });

export const createCheckout = (quantity: number) =>
  req<{ url: string; id: string }>("/api/billing/checkout", {
    method: "POST",
    body: JSON.stringify({
      quantity,
      successUrl: `${window.location.origin}/billing?checkout=success`,
      cancelUrl: `${window.location.origin}/billing?status=cancel`,
    }),
  });

export type FreeTierStatus = {
  paid: boolean;
  connections: number;
  connectionsUsed: number;
  callsLimit: number;
  callsUsed: number;
  chatsLimit: number;
  chatsUsed: number;
  week: string;
  alerts?: FreeTierAlert[];
};

export type FreeTierAlert = {
  kind: "free_calls" | "free_chats" | "free_connections" | string;
  threshold: number; // 80 ou 100
  week: string;
  createdAt: number;
};

export const getFreeTier = () => req<FreeTierStatus>("/api/billing/free-tier");

export type FreeTierLimits = { connections: number; callsWeek: number; chatsWeek: number };
export const getFreeTierLimits = () => req<FreeTierLimits>("/api/billing/free-tier/limits");
export const saveFreeTierLimits = (partial: Partial<FreeTierLimits>) =>
  req<FreeTierLimits>("/api/billing/free-tier/limits", {
    method: "PUT",
    body: JSON.stringify(partial),
  });