import { useCallback, useEffect, useRef, useState } from "react";

export type RecState = "idle" | "recording" | "paused" | "stopped";

type Args = {
  audio?: MediaStream | null;
  video?: MediaStream | null;
};

const pickMime = (hasVideo: boolean): string => {
  const cands = hasVideo
    ? ["video/webm;codecs=vp9,opus", "video/webm;codecs=vp8,opus", "video/webm"]
    : ["audio/webm;codecs=opus", "audio/webm", "audio/ogg;codecs=opus"];
  for (const m of cands) {
    if (typeof MediaRecorder !== "undefined" && MediaRecorder.isTypeSupported(m)) return m;
  }
  return "";
};

export const useMediaRecording = ({ audio, video }: Args) => {
  const [state, setState] = useState<RecState>("idle");
  const [blobUrl, setBlobUrl] = useState<string | null>(null);
  const [size, setSize] = useState(0);
  const [elapsed, setElapsed] = useState(0);
  const chunksRef = useRef<Blob[]>([]);
  const recRef = useRef<MediaRecorder | null>(null);
  const mimeRef = useRef<string>("");
  const startedRef = useRef<number>(0);
  const pausedAccRef = useRef<number>(0);
  const pauseStartRef = useRef<number>(0);

  const supported = typeof MediaRecorder !== "undefined" && (!!audio || !!video);

  useEffect(() => {
    if (state !== "recording") return;
    const t = setInterval(() => {
      setElapsed(Date.now() - startedRef.current - pausedAccRef.current);
    }, 500);
    return () => clearInterval(t);
  }, [state]);

  useEffect(() => () => {
    try { recRef.current?.stop(); } catch {}
    if (blobUrl) URL.revokeObjectURL(blobUrl);
  }, [blobUrl]);

  const start = useCallback(() => {
    if (!supported || recRef.current) return;
    const tracks: MediaStreamTrack[] = [];
    if (video) tracks.push(...video.getVideoTracks());
    if (audio) tracks.push(...audio.getAudioTracks());
    if (tracks.length === 0) return;
    const merged = new MediaStream(tracks);
    const mime = pickMime(!!video);
    mimeRef.current = mime;
    const rec = new MediaRecorder(merged, mime ? { mimeType: mime } : undefined);
    chunksRef.current = [];
    setSize(0);
    rec.ondataavailable = (ev) => {
      if (ev.data && ev.data.size > 0) {
        chunksRef.current.push(ev.data);
        setSize((s) => s + ev.data.size);
      }
    };
    rec.onstop = () => {
      const blob = new Blob(chunksRef.current, { type: mime || "application/octet-stream" });
      if (blobUrl) URL.revokeObjectURL(blobUrl);
      setBlobUrl(URL.createObjectURL(blob));
      setState("stopped");
      recRef.current = null;
    };
    rec.start(1000);
    recRef.current = rec;
    startedRef.current = Date.now();
    pausedAccRef.current = 0;
    setElapsed(0);
    if (blobUrl) { URL.revokeObjectURL(blobUrl); setBlobUrl(null); }
    setState("recording");
  }, [supported, audio, video, blobUrl]);

  const pause = useCallback(() => {
    const r = recRef.current;
    if (!r || r.state !== "recording") return;
    r.pause();
    pauseStartRef.current = Date.now();
    setState("paused");
  }, []);

  const resume = useCallback(() => {
    const r = recRef.current;
    if (!r || r.state !== "paused") return;
    r.resume();
    pausedAccRef.current += Date.now() - pauseStartRef.current;
    setState("recording");
  }, []);

  const stop = useCallback(() => {
    const r = recRef.current;
    if (!r) return;
    try { r.stop(); } catch {}
  }, []);

  const extension = mimeRef.current.startsWith("video/") ? "webm" : mimeRef.current.includes("ogg") ? "ogg" : "webm";

  return { state, start, pause, resume, stop, blobUrl, size, elapsed, supported, extension };
};