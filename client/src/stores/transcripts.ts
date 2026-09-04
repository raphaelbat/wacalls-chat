import { create } from "zustand";
import {
  listTranscripts,
  transcribe as requestTranscribe,
  type Transcript,
  type TranscriptScope,
} from "@/services/transcripts";

type State = {
  /** key = `${scope}:${refId}` */
  byRef: Record<string, Transcript>;
  pending: Record<string, boolean>;
  enabled: boolean;
  loadedChats: Record<string, boolean>;
};

export const refKey = (scope: TranscriptScope, refId: string): string => `${scope}:${refId}`;

export const useTranscripts = create<State>(() => ({
  byRef: {},
  pending: {},
  enabled: true,
  loadedChats: {},
}));

const merge = (rows: Transcript[]): void => {
  if (!rows.length) return;
  useTranscripts.setState((s) => {
    const byRef = { ...s.byRef };
    for (const t of rows) byRef[refKey(t.scope, t.refId)] = t;
    return { byRef };
  });
};

/** Loads every transcript already stored for a conversation (once per chat). */
export const loadChatTranscripts = async (sessionId: string, chatJid: string): Promise<void> => {
  const key = `${sessionId}|${chatJid}`;
  if (!sessionId || !chatJid || useTranscripts.getState().loadedChats[key]) return;
  useTranscripts.setState((s) => ({ loadedChats: { ...s.loadedChats, [key]: true } }));
  try {
    const r = await listTranscripts({ sessionId, chatJid });
    merge(r.transcripts ?? []);
    useTranscripts.setState({ enabled: r.enabled !== false });
  } catch {
    /* silencioso: transcrição é opcional */
  }
};

/** Loads transcripts of saved call recordings (reports page). */
export const loadRecordingTranscripts = async (sessionId?: string): Promise<void> => {
  try {
    const r = await listTranscripts({ sessionId, scope: "recording" });
    merge(r.transcripts ?? []);
    useTranscripts.setState({ enabled: r.enabled !== false });
  } catch {
    /* ignore */
  }
};

export const getTranscript = (scope: TranscriptScope, refId: string): Transcript | undefined =>
  useTranscripts.getState().byRef[refKey(scope, refId)];

/** Runs the transcription on the server and caches the result. */
export const runTranscription = async (input: {
  sessionId?: string;
  scope: TranscriptScope;
  refId: string;
  chatJid?: string;
  force?: boolean;
}): Promise<Transcript> => {
  const key = refKey(input.scope, input.refId);
  useTranscripts.setState((s) => ({ pending: { ...s.pending, [key]: true } }));
  try {
    const t = await requestTranscribe(input);
    merge([t]);
    return t;
  } finally {
    useTranscripts.setState((s) => {
      const pending = { ...s.pending };
      delete pending[key];
      return { pending };
    });
  }
};
