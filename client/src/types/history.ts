export type RecordingRef = {
  callId: string;
  mime: string;
  size: number;
  token: string;
  uploadedAt: number;
};

export type HistoryRow = {
  callId: string;
  peer: string;
  direction: string;
  startedAt: number;
  endedAt: number | null;
  endReason: string | null;
  recording?: RecordingRef | null;
};
