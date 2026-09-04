import { apiUrl } from "@/lib/api-base";

/**
 * Campanhas de mídia: uma mensagem para uma lista de contatos, com ritmo
 * controlado para não queimar o número.
 */

export type CampaignStatus = "draft" | "running" | "paused" | "finished";
export type MediaKind = "image" | "video" | "audio" | "document";

export interface Campaign {
  id: string;
  name: string;
  status: CampaignStatus;

  text: string;
  mediaUrl: string;
  mediaKind: MediaKind | "";
  filename: string;

  /** Números do rodízio, separados por vírgula. Vazio = todos os conectados. */
  sessionIds: string;

  minIntervalSec: number;
  maxIntervalSec: number;
  /** Por número. 0 = sem teto. */
  perHour: number;
  perDay: number;

  /** Hora local; início igual ao fim libera o dia inteiro. */
  windowStart: number;
  windowEnd: number;
  /** "1,2,3,4,5,6" — 0 é domingo. */
  weekdays: string;

  warmup: boolean;

  createdAt: number;
  updatedAt: number;
  startedAt: number;
  finishedAt: number;
  lastError: string;
}

export interface CampaignProgress {
  total: number;
  pending: number;
  sent: number;
  failed: number;
  skipped: number;
}

export interface CampaignTarget {
  id: number;
  campaignId: string;
  jid: string;
  name: string;
  status: "pending" | "sent" | "failed" | "skipped";
  sessionId: string;
  error: string;
  sentAt: number;
  attempts: number;
}

export interface CampaignComProgresso {
  campaign: Campaign;
  progress: CampaignProgress;
}

async function http<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(apiUrl(path), {
    ...init,
    credentials: "include",
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
  });
  if (!res.ok) {
    // O servidor responde {"error": "..."} — mostrar essa frase é melhor do
    // que mostrar o código HTTP para quem só quer disparar uma campanha.
    const txt = await res.text().catch(() => "");
    let msg = txt || String(res.status);
    try {
      const j = JSON.parse(txt) as { error?: string };
      if (j?.error) msg = j.error;
    } catch {
      /* corpo não era JSON: fica o texto cru mesmo */
    }
    throw new Error(msg);
  }
  return res.json() as Promise<T>;
}

export const listCampaigns = () =>
  http<{ campaigns: CampaignComProgresso[] }>("/api/campaigns").then((r) => r.campaigns ?? []);

export const getCampaign = (id: string) => http<CampaignComProgresso>(`/api/campaigns/${id}`);

export const createCampaign = (body: Partial<Campaign>) =>
  http<Campaign>("/api/campaigns", { method: "POST", body: JSON.stringify(body) });

export const updateCampaign = (id: string, body: Partial<Campaign>) =>
  http<Campaign>(`/api/campaigns/${id}`, { method: "PUT", body: JSON.stringify(body) });

export const deleteCampaign = (id: string) =>
  http<{ ok: string }>(`/api/campaigns/${id}`, { method: "DELETE" });

export const listTargets = (id: string, status?: string) =>
  http<{ targets: CampaignTarget[] }>(
    `/api/campaigns/${id}/targets${status ? `?status=${status}` : ""}`,
  ).then((r) => r.targets ?? []);

export const addTargets = (id: string, targets: Array<{ jid: string; name: string }>) =>
  http<{ adicionados: number; recebidos: number; progress: CampaignProgress }>(
    `/api/campaigns/${id}/targets`,
    { method: "POST", body: JSON.stringify({ targets }) },
  );

export const clearTargets = (id: string) =>
  http<{ ok: string }>(`/api/campaigns/${id}/targets`, { method: "DELETE" });

export const startCampaign = (id: string) =>
  http<{ ok: string }>(`/api/campaigns/${id}/start`, { method: "POST" });

export const pauseCampaign = (id: string) =>
  http<{ ok: string }>(`/api/campaigns/${id}/pause`, { method: "POST" });

/** Conversas marcadas com uma tag — usado para montar a lista da campanha. */
export const chatsComTag = (tagId: string) =>
  http<{ chats: Array<{ sessionId: string; chatJid: string }> }>(`/api/tags/${tagId}/chats`).then(
    (r) => r.chats ?? [],
  );

/** Sobe o arquivo da campanha reaproveitando o upload dos fluxos. */
export const uploadCampaignMedia = async (file: File) => {
  const fd = new FormData();
  fd.append("file", file);
  const res = await fetch(apiUrl("/api/flow/assets"), {
    method: "POST",
    credentials: "include",
    body: fd,
  });
  if (!res.ok) throw new Error(await res.text().catch(() => String(res.status)));
  return (await res.json()) as { url: string; mime: string; filename: string; size: number };
};

/** Adivinha o tipo de mídia pelo MIME, para o usuário não ter que escolher. */
export const tipoDaMidia = (mime: string, filename = ""): MediaKind => {
  const m = (mime || "").toLowerCase();
  if (m.startsWith("image/")) return "image";
  if (m.startsWith("video/")) return "video";
  if (m.startsWith("audio/")) return "audio";
  const ext = filename.toLowerCase().split(".").pop() ?? "";
  if (["jpg", "jpeg", "png", "webp", "gif"].includes(ext)) return "image";
  if (["mp4", "mov", "webm"].includes(ext)) return "video";
  if (["mp3", "ogg", "opus", "m4a", "wav"].includes(ext)) return "audio";
  return "document";
};

/**
 * Estimativa de quanto a campanha vai demorar.
 *
 * Serve para o usuário descobrir ANTES de disparar que 4.000 contatos a um
 * envio por minuto levam dias — e não no meio do caminho.
 */
export const estimativa = (
  pendentes: number,
  c: Pick<Campaign, "minIntervalSec" | "maxIntervalSec" | "perHour" | "perDay" | "sessionIds">,
  numerosConectados: number,
): string => {
  if (pendentes <= 0) return "";
  const numeros = Math.max(
    1,
    c.sessionIds ? c.sessionIds.split(",").filter(Boolean).length : numerosConectados,
  );
  const medio = (c.minIntervalSec + c.maxIntervalSec) / 2 || 45;

  // Duas restrições disputam: o intervalo entre envios e o teto por hora.
  const porHoraPeloIntervalo = 3600 / medio;
  const porHoraPeloTeto = c.perHour > 0 ? c.perHour * numeros : Infinity;
  const porHora = Math.min(porHoraPeloIntervalo, porHoraPeloTeto);

  const horas = pendentes / porHora;
  const diasPeloTetoDiario = c.perDay > 0 ? pendentes / (c.perDay * numeros) : 0;
  const dias = Math.max(horas / 24, diasPeloTetoDiario);

  if (dias >= 1.5) return `~${Math.ceil(dias)} dias de disparo`;
  if (horas >= 1.5) return `~${Math.ceil(horas)} horas de disparo`;
  return `~${Math.max(1, Math.ceil(horas * 60))} minutos de disparo`;
};
