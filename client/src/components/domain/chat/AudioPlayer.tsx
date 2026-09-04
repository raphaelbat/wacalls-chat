import { useEffect, useMemo, useRef, useState } from "react";
import { Pause, Play } from "lucide-react";

interface Props {
  src: string;
  mine?: boolean;
}

const BARS = 38;

function pseudoBars(seed: string): number[] {
  let h = 0;
  for (let i = 0; i < seed.length; i++) h = (h * 31 + seed.charCodeAt(i)) >>> 0;
  const out: number[] = [];
  for (let i = 0; i < BARS; i++) {
    h = (h * 1103515245 + 12345) >>> 0;
    const v = ((h >>> 16) & 0x7fff) / 0x7fff;
    out.push(0.25 + v * 0.75);
  }
  return out;
}

function fmt(s: number): string {
  if (!isFinite(s) || s < 0) s = 0;
  const m = Math.floor(s / 60);
  const r = Math.floor(s % 60);
  return `${m}:${r.toString().padStart(2, "0")}`;
}

export const AudioPlayer = ({ src, mine }: Props) => {
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const [playing, setPlaying] = useState(false);
  const [duration, setDuration] = useState(0);
  const [current, setCurrent] = useState(0);
  const bars = useMemo(() => pseudoBars(src), [src]);

  useEffect(() => {
    const a = audioRef.current;
    if (!a) return;
    const onTime = () => setCurrent(a.currentTime);
    const onMeta = () => setDuration(isFinite(a.duration) ? a.duration : 0);
    const onEnd = () => {
      setPlaying(false);
      setCurrent(0);
    };
    a.addEventListener("timeupdate", onTime);
    a.addEventListener("loadedmetadata", onMeta);
    a.addEventListener("durationchange", onMeta);
    a.addEventListener("ended", onEnd);
    return () => {
      a.removeEventListener("timeupdate", onTime);
      a.removeEventListener("loadedmetadata", onMeta);
      a.removeEventListener("durationchange", onMeta);
      a.removeEventListener("ended", onEnd);
    };
  }, []);

  const toggle = async () => {
    const a = audioRef.current;
    if (!a) return;
    if (a.paused) {
      await a.play();
      setPlaying(true);
    } else {
      a.pause();
      setPlaying(false);
    }
  };

  const seek = (e: React.MouseEvent<HTMLDivElement>) => {
    const a = audioRef.current;
    if (!a || !duration) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const pct = Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width));
    a.currentTime = pct * duration;
    setCurrent(a.currentTime);
  };

  const progress = duration > 0 ? current / duration : 0;
  const playedColor = "bg-emerald-500";
  const restColor = mine ? "bg-emerald-200/60" : "bg-zinc-400/60 dark:bg-zinc-500/60";

  return (
    <div className="mb-1 flex flex-col items-center gap-1.5">
      <div className="flex w-[260px] max-w-full items-center gap-3 rounded-full bg-black/5 px-2 py-1.5 dark:bg-white/5">
        <button
          type="button"
          onClick={toggle}
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-emerald-500 text-white shadow hover:bg-emerald-600"
          aria-label={playing ? "Pausar" : "Reproduzir"}
        >
          {playing ? <Pause className="h-4 w-4" /> : <Play className="ml-0.5 h-4 w-4" />}
        </button>
        <div className="flex flex-1 flex-col gap-1">
          <div className="flex h-8 cursor-pointer items-center gap-[2px]" onClick={seek}>
            {bars.map((h, i) => {
              const filled = i / BARS <= progress;
              return (
                <span
                  key={i}
                  className={`w-[2px] rounded-full ${filled ? playedColor : restColor}`}
                  style={{ height: `${Math.round(h * 100)}%` }}
                />
              );
            })}
          </div>
          <div className="flex justify-between px-0.5 text-[10px] tabular-nums text-muted-foreground">
            <span>{fmt(playing || current > 0 ? current : 0)}</span>
            <span>{fmt(duration)}</span>
          </div>
        </div>
        <audio ref={audioRef} src={src} preload="metadata" className="hidden" />
      </div>
    </div>
  );
};
