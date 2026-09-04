import { apiGet, apiPost } from "@/lib/api";

export type TranscriptScope = "message" | "recording";

export type Transcript = {
  id: string;
  sessionId: string;
  scope: TranscriptScope;
  refId: string;
  chatJid?: string;
  text: string;
  lang?: string;
  engine?: string;
  status: "done" | "empty" | "error";
  error?: string;
  createdAt: number;
};

export const listTranscripts = (params: { sessionId?: string; chatJid?: string; scope?: TranscriptScope }) => {
  const q = new URLSearchParams();
  if (params.sessionId) q.set("sessionId", params.sessionId);
  if (params.chatJid) q.set("chatJid", params.chatJid);
  if (params.scope) q.set("scope", params.scope);
  return apiGet<{ transcripts: Transcript[]; enabled: boolean }>(`/api/transcripts?${q.toString()}`);
};

export const searchTranscripts = (params: { sessionId?: string; chatJid?: string; q: string }) => {
  const s = new URLSearchParams();
  if (params.sessionId) s.set("sessionId", params.sessionId);
  if (params.chatJid) s.set("chatJid", params.chatJid);
  s.set("q", params.q);
  return apiGet<{ transcripts: Transcript[] }>(`/api/transcripts/search?${s.toString()}`).then(
    (r) => r.transcripts ?? [],
  );
};

/** Requests (or re-requests) the transcription of one audio artifact. */
export const transcribe = (input: {
  sessionId?: string;
  scope: TranscriptScope;
  refId: string;
  chatJid?: string;
  force?: boolean;
}) => apiPost<Transcript>("/api/transcripts", input);
