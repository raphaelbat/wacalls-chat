import { apiGet, apiPost, apiDelete } from "@/lib/api";
import { apiUrl } from "@/lib/api-base";
import { getClientId } from "@/lib/client-id";

export type CampaignKind = "audio" | "flow" | "ai";
export type CampaignStatus = "draft" | "running" | "paused" | "finished";
export type CampaignContactStatus =
  | "pending"
  | "dialing"
  | "answered"
  | "no_answer"
  | "failed";

export interface Campaign {
  id: string;
  ownerId: string;
  name: string;
  sessionId: string;
  kind: CampaignKind;
  audioId?: string;
  flowId?: string;
  status: CampaignStatus;
  concurrent: number;
  startDelaySec?: number;
  createdAt: number;
  updatedAt: number;
  total: number;
  pending: number;
  dialing: number;
  answered: number;
  noAnswer: number;
  failed: number;
}

export interface CampaignAudio {
  id: string;
  ownerId?: string;
  name: string;
  path: string;
  mime: string;
  bytes: number;
  durationSec: number;
  createdAt: number;
}

export interface CampaignContact {
  id: string;
  campaignId: string;
  phone: string;
  name?: string;
  status: CampaignContactStatus;
  callId?: string;
  attempts: number;
  lastError?: string;
  updatedAt: number;
}

export const listCampaigns = () =>
  apiGet<{ campaigns: Campaign[] }>("/api/campaigns").then((r) => r.campaigns ?? []);

export const getCampaign = (id: string) => apiGet<Campaign>(`/api/campaigns/${id}`);

export const createCampaign = (body: {
  name: string;
  sessionId: string;
  kind: CampaignKind;
  audioId?: string;
  flowId?: string;
  concurrent?: number;
  startDelaySec?: number;
  endOnAudioEnd?: boolean;
  contacts?: { phone: string; name?: string }[];
}) => apiPost<Campaign>("/api/campaigns", body);

export const deleteCampaign = (id: string) => apiDelete(`/api/campaigns/${id}`);

export const startCampaign = (id: string) =>
  apiPost<{ status: string }>(`/api/campaigns/${id}/start`, {});

export const pauseCampaign = (id: string) =>
  apiPost<{ status: string }>(`/api/campaigns/${id}/pause`, {});

export const listContacts = (id: string) =>
  apiGet<{ contacts: CampaignContact[] }>(`/api/campaigns/${id}/contacts`).then(
    (r) => r.contacts ?? [],
  );

export const addContacts = (id: string, contacts: { phone: string; name?: string }[]) =>
  apiPost<{ added: number }>(`/api/campaigns/${id}/contacts`, { contacts });

export const listAudios = () =>
  apiGet<{ audios: CampaignAudio[] }>("/api/campaign-audios").then((r) => r.audios ?? []);

export const uploadAudio = async (file: File, name?: string): Promise<CampaignAudio> => {
  const form = new FormData();
  form.append("file", file);
  if (name) form.append("name", name);
  const r = await fetch(apiUrl("/api/campaign-audios"), {
    method: "POST",
    credentials: "include",
    headers: { "X-Client-Id": getClientId() },
    body: form,
  });
  if (!r.ok) throw new Error(`upload ${r.status} ${await r.text().catch(() => "")}`);
  return r.json();
};

export const deleteAudio = (id: string) => apiDelete(`/api/campaign-audios/${id}`);

/** Normalize a free-form list of phone numbers (one per line or comma-separated). */
export function parsePhoneList(raw: string): { phone: string }[] {
  return raw
    .split(/[\n,;]+/)
    .map((p) => p.replace(/[^\d+]/g, ""))
    .filter((p) => p.length >= 8)
    .map((phone) => ({ phone }));
}
