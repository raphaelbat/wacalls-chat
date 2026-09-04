import { apiUrl } from "@/lib/api-base";
import { getClientId } from "@/lib/client-id";

export type HoldMusicInfo = {
  key: string;
  exists: boolean;
  sizeBytes?: number;
  updatedAt?: number;
  url?: string;
  config?: HoldMusicConfig;
};

// Decode any browser-supported audio (mp3/wav/ogg/m4a) via WebAudio, resample
// to 16 kHz mono, and re-encode as 16-bit little-endian PCM WAV. The server
// accepts only this canonical shape so it never has to decode/resample.
const toCanonicalWAV = async (file: File): Promise<Blob> => {
  const raw = await file.arrayBuffer();
  const decodeCtx = new (window.AudioContext || (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext)();
  let decoded: AudioBuffer;
  try {
    decoded = await decodeCtx.decodeAudioData(raw.slice(0));
  } catch (e) {
    await decodeCtx.close();
    throw new Error("Não foi possível decodificar o áudio (tente MP3, WAV, OGG ou M4A)");
  }
  await decodeCtx.close();

  // Mixdown to mono at source rate.
  const srcLen = decoded.length;
  const monoSrc = new Float32Array(srcLen);
  for (let ch = 0; ch < decoded.numberOfChannels; ch++) {
    const data = decoded.getChannelData(ch);
    for (let i = 0; i < srcLen; i++) monoSrc[i] += data[i] / decoded.numberOfChannels;
  }

  // Resample mono to 16000 Hz using OfflineAudioContext.
  const targetRate = 16000;
  const outLen = Math.ceil((srcLen * targetRate) / decoded.sampleRate);
  const off = new OfflineAudioContext(1, outLen, targetRate);
  const buf = off.createBuffer(1, srcLen, decoded.sampleRate);
  buf.copyToChannel(monoSrc, 0);
  const src = off.createBufferSource();
  src.buffer = buf;
  src.connect(off.destination);
  src.start(0);
  const rendered = await off.startRendering();
  const pcm = rendered.getChannelData(0);

  // Encode WAV s16le mono @ 16000 Hz.
  const dataBytes = pcm.length * 2;
  const buffer = new ArrayBuffer(44 + dataBytes);
  const view = new DataView(buffer);
  const writeStr = (o: number, s: string) => { for (let i = 0; i < s.length; i++) view.setUint8(o + i, s.charCodeAt(i)); };
  writeStr(0, "RIFF");
  view.setUint32(4, 36 + dataBytes, true);
  writeStr(8, "WAVE");
  writeStr(12, "fmt ");
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true);            // PCM
  view.setUint16(22, 1, true);            // mono
  view.setUint32(24, targetRate, true);
  view.setUint32(28, targetRate * 2, true);
  view.setUint16(32, 2, true);            // block align
  view.setUint16(34, 16, true);           // bits per sample
  writeStr(36, "data");
  view.setUint32(40, dataBytes, true);
  let offset = 44;
  for (let i = 0; i < pcm.length; i++, offset += 2) {
    const s = Math.max(-1, Math.min(1, pcm[i]));
    view.setInt16(offset, s < 0 ? s * 0x8000 : s * 0x7fff, true);
  }
  return new Blob([buffer], { type: "audio/wav" });
};

const endpoint = (queueId?: string) =>
  queueId ? `/api/holdmusic/queue/${queueId}` : `/api/holdmusic/global`;

export const uploadHoldMusic = async (file: File, queueId?: string): Promise<HoldMusicInfo> => {
  const wav = await toCanonicalWAV(file);
  const fd = new FormData();
  fd.append("file", wav, "holdmusic.wav");
  const r = await fetch(apiUrl(endpoint(queueId)), {
    method: "POST",
    headers: { "X-Client-Id": getClientId() },
    credentials: "include",
    body: fd,
  });
  if (!r.ok) {
    const text = await r.text().catch(() => "");
    throw new Error(`upload ${r.status} ${text}`);
  }
  return r.json();
};

export const getHoldMusic = async (queueId?: string): Promise<HoldMusicInfo> => {
  const r = await fetch(apiUrl(endpoint(queueId)), { credentials: "include" });
  if (!r.ok) throw new Error(`get ${r.status}`);
  return r.json();
};

export const deleteHoldMusic = async (queueId?: string): Promise<void> => {
  const r = await fetch(apiUrl(endpoint(queueId)), {
    method: "DELETE",
    headers: { "X-Client-Id": getClientId() },
    credentials: "include",
  });
  if (!r.ok) throw new Error(`delete ${r.status}`);
};

export const holdMusicPreviewUrl = (queueId?: string): string =>
  apiUrl(`/api/holdmusic/file/${queueId ? `queue_${queueId}` : "global"}`);

export type HoldMusicConfig = {
  volume: number;
  fadeInMs: number;
  fadeOutMs: number;
};

export const defaultHoldConfig = (): HoldMusicConfig => ({ volume: 1, fadeInMs: 800, fadeOutMs: 500 });

const configEndpoint = (queueId?: string) =>
  queueId ? `/api/holdmusic/queue/${queueId}/config` : `/api/holdmusic/global/config`;

export const saveHoldMusicConfig = async (cfg: HoldMusicConfig, queueId?: string): Promise<HoldMusicConfig> => {
  const r = await fetch(apiUrl(configEndpoint(queueId)), {
    method: "PUT",
    headers: { "Content-Type": "application/json", "X-Client-Id": getClientId() },
    credentials: "include",
    body: JSON.stringify(cfg),
  });
  if (!r.ok) throw new Error(`config ${r.status}`);
  const data = await r.json();
  return data.config as HoldMusicConfig;
};
