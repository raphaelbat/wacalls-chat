import { apiPost, apiDelete } from "@/lib/api";
import { getClientId } from "@/lib/client-id";
import { apiUrl } from "@/lib/api-base";
import type { RecordingRef } from "@/types/history";

export const startCall = (sid: string, phone: string, record: boolean, video: boolean) =>
  apiPost<{ call: { callId: string } }>(`/api/sessions/${sid}/calls`, {
    phone,
    duration_ms: 300_000,
    record,
    video,
  });

export const acceptCall = (sid: string, callId: string) =>
  apiPost<{ call: { callId: string } }>(`/api/sessions/${sid}/calls/${callId}/accept`, {});

export const rejectCall = async (sid: string, callId: string): Promise<void> => {
  const r = await fetch(apiUrl(`/api/sessions/${sid}/calls/${callId}/reject`), {
    method: "POST",
    headers: { "X-Client-Id": getClientId(), "Content-Type": "application/json" },
    credentials: "include",
    body: "{}",
  });
  if (!r.ok) throw new Error(`reject ${r.status}`);
};

export const endCall = (sid: string, callId: string) =>
  apiDelete(`/api/sessions/${sid}/calls/${callId}`);

export type SignedLinks = {
  token: string;
  mime?: string;
  size?: number;
  shareUrl: string;
  downloadUrl: string;
  expiresAt: number;
};

export type UploadRecordingResponse = RecordingRef & SignedLinks;

export const uploadCallRecording = async (
  sid: string,
  callId: string,
  blob: Blob,
  filename: string,
): Promise<UploadRecordingResponse> => {
  const fd = new FormData();
  fd.append("file", blob, filename);
  fd.append("mime", blob.type || "application/octet-stream");
  const r = await fetch(apiUrl(`/api/sessions/${sid}/calls/${callId}/recording`), {
    method: "POST",
    headers: { "X-Client-Id": getClientId() },
    credentials: "include",
    body: fd,
  });
  if (!r.ok) {
    const text = await r.text().catch(() => "");
    throw new Error(`upload ${r.status} ${text}`);
  }
  return r.json() as Promise<UploadRecordingResponse>;
};

// Request a fresh short-lived signed URL pair (view + download).
// ttlSeconds is clamped server-side to <= 24h.
export const signCallRecording = async (
  sid: string,
  callId: string,
  ttlSeconds = 600,
): Promise<SignedLinks> => {
  const r = await fetch(
    apiUrl(`/api/sessions/${sid}/calls/${callId}/recording/sign?ttl=${ttlSeconds}`),
    {
      method: "POST",
      headers: { "X-Client-Id": getClientId() },
      credentials: "include",
    },
  );
  if (!r.ok) {
    const text = await r.text().catch(() => "");
    throw new Error(`sign ${r.status} ${text}`);
  }
  return r.json() as Promise<SignedLinks>;
};

export const absoluteUrl = (path: string): string => {
  const u = apiUrl(path);
  if (/^https?:\/\//i.test(u)) return u;
  if (typeof window !== "undefined") return `${window.location.origin}${u}`;
  return u;
};

export const holdCall = (sid: string, callId: string, on: boolean) =>
  apiPost<{ ok: boolean; on: boolean }>(`/api/sessions/${sid}/calls/${callId}/hold`, { on });

export const transferCall = (
  sid: string,
  callId: string,
  targetType: "user" | "queue",
  targetId: string,
  note?: string,
) =>
  apiPost<{ ok: boolean; delivered: number }>(
    `/api/sessions/${sid}/calls/${callId}/transfer`,
    { targetType, targetId, note: note ?? "" },
  );
