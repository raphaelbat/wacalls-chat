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
      if (j?.error) msg = j.error;
    } catch {
      /* ignore */
    }
    throw new Error(msg);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

export type Plan = {
  id: string;
  nome: string;
  publico: boolean;
  trial: boolean;
  diasTrial: number;
  usuarios: number;
  conexoes: number;
  filas: number;
  valor: number;
  // Período de cobrança e status do plano (novos campos opcionais — antigos
  // planos continuam compatíveis pois caem nos defaults da UI).
  periodo?: "mensal" | "trimestral" | "semestral" | "anual";
  ativo?: boolean;
  recursos: Record<string, boolean>;
};

export const listPlans = () =>
  req<{ plans: Plan[] }>("/api/settings/plans").then((r) => r.plans ?? []);

export const savePlan = (plan: Plan) =>
  req<Plan>(
    plan.id ? `/api/settings/plans/${plan.id}` : "/api/settings/plans",
    { method: plan.id ? "PUT" : "POST", body: JSON.stringify(plan) },
  );

export const deletePlan = (id: string) =>
  req<void>(`/api/settings/plans/${id}`, { method: "DELETE" });

export type ActivePlan = { id: string; plan: Plan | null };
export const getActivePlan = () => req<ActivePlan>("/api/settings/active-plan");
export const setActivePlan = (id: string) =>
  req<{ id: string }>("/api/settings/active-plan", {
    method: "PUT",
    body: JSON.stringify({ id }),
  });

// Matriz de recursos do plano ativo (atalho para leitura/escrita sem
// precisar abrir o editor de planos inteiro).
export type ActiveMatrix = { id: string; recursos: Record<string, boolean> };
export const getActiveMatrix = () =>
  req<ActiveMatrix>("/api/settings/active-plan/matrix");
export const saveActiveMatrix = (recursos: Record<string, boolean>) =>
  req<ActiveMatrix>("/api/settings/active-plan/matrix", {
    method: "PUT",
    body: JSON.stringify({ recursos }),
  });

// Uso atual vs. limites do plano ativo. Limite 0 = ilimitado (mesma
// convenção do formulário). A propriedade `updatedAt` é um epoch em segundos
// que pode ser usada para diferenciar polls consecutivos.
export type PlanUsage = {
  limits: { usuarios: number; conexoes: number; filas: number };
  usage: { usuarios: number; conexoes: number; filas: number };
  updatedAt: number;
};
export const getPlanUsage = () => req<PlanUsage>("/api/settings/plan-usage");

export type Options = {
  systemName?: string;
  supportEmail?: string;
  docUrl?: string;
  environment?: string;
  // Toggles exibidos na aba Opções. Persistidos junto com o resto do
  // payload JSON em settings/options no backend (não requer migração).
  googleLoginEnabled?: boolean;
  ratingsEnabled?: boolean;
  randomAgentEnabled?: boolean;
  transferNotifyEnabled?: boolean;
  // Notificações
  muteAllNotifications?: boolean;
  notificationSoundEnabled?: boolean;
  notificationSound?: string; // chave do preset ou URL custom
  // Quando true, exige que o atendente registre um motivo ao encerrar
  // o atendimento (tanto pelo botão Finalizar quanto pelo X da lista).
  requireCloseReason?: boolean;
  // Quando false, o botão "Transcrever" não aparece em nenhuma mensagem
  // de áudio do sistema e o endpoint deixa de ser chamado. Controlado
  // somente pelo admin do SaaS (aba Opções).
  transcriptionEnabled?: boolean;
  // Avaliação (CSAT). Quando ratingsEnabled = true, controla o modo de
  // coleta e os rótulos exibidos para o cliente.
  ratingMode?: "selection" | "comment" | "both";
  ratingOptions?: Array<{ value: number; label: string }>;
};

export const getOptions = () => req<Options>("/api/settings/options");
export const saveOptions = (opts: Options) =>
  req<Options>("/api/settings/options", {
    method: "PUT",
    body: JSON.stringify(opts),
  });

/* ------------- SMTP (super admin) ------------- */

export type SmtpConfig = {
  host?: string;
  port?: string;
  user?: string;
  from?: string;
  pass?: string; // só escrita
  passSet?: boolean;
  configured?: boolean;
};

export const getSMTP = () => req<SmtpConfig>("/api/billing/smtp");
export const saveSMTP = (cfg: SmtpConfig) =>
  req<SmtpConfig>("/api/billing/smtp", {
    method: "PUT",
    body: JSON.stringify(cfg),
  });
export const testSMTP = (to: string) =>
  req<{ ok: boolean; message?: string }>("/api/billing/smtp/test", {
    method: "POST",
    body: JSON.stringify({ to }),
  });

export type Whitelabel = {
  appName?: string;
  appNameMobile?: string;
  primaryLight?: string;
  primaryDark?: string;
  logoLight?: string;
  logoDark?: string;
  favicon?: string;
  logoMobile?: string;
  bgLight?: string;
  bgDark?: string;
  splash?: string;
};

export const getWhitelabel = () => req<Whitelabel>("/api/settings/whitelabel");
export const saveWhitelabel = (wl: Whitelabel) =>
  req<Whitelabel>("/api/settings/whitelabel", {
    method: "PUT",
    body: JSON.stringify(wl),
  });

export const uploadWhitelabelAsset = async (
  kind: keyof Whitelabel,
  file: File,
): Promise<{ url: string; kind: string }> => {
  const fd = new FormData();
  fd.append("kind", String(kind));
  fd.append("file", file);
  const res = await fetch(apiUrl("/api/settings/whitelabel/asset"), {
    method: "POST",
    credentials: "include",
    body: fd,
  });
  if (!res.ok) {
    let msg = `${res.status}`;
    try {
      const j = await res.json();
      if (j?.error) msg = j.error;
    } catch {
      /* ignore */
    }
    throw new Error(msg);
  }
  return res.json();
};

/* ------------- Google OAuth ------------- */

export type GoogleOAuthPublic = { enabled: boolean; clientId: string };
export type GoogleOAuthAdmin = {
  enabled: boolean;
  clientId: string;
  redirectUri: string;
  hasSecret: boolean;
};
export type GoogleOAuthUpdate = {
  enabled: boolean;
  clientId: string;
  clientSecret?: string;
  redirectUri?: string;
};

export const getGoogleOAuthPublic = () =>
  req<GoogleOAuthPublic>("/api/auth/google/config");

export const getGoogleOAuth = () =>
  req<GoogleOAuthAdmin>("/api/settings/google-oauth");

export const saveGoogleOAuth = (cfg: GoogleOAuthUpdate) =>
  req<GoogleOAuthAdmin>("/api/settings/google-oauth", {
    method: "PUT",
    body: JSON.stringify(cfg),
  });