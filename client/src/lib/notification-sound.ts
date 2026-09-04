// Reproduz um som curto de notificação sintetizado via Web Audio API.
// Sem asset externo — dois beeps rápidos em tons agradáveis (E5/A5).
// Respeita throttling para não sobrepor várias mensagens em rajada.

let ctx: AudioContext | null = null;
let lastPlayed = 0;
const THROTTLE_MS = 400;

const getCtx = (): AudioContext | null => {
  if (typeof window === "undefined") return null;
  try {
    if (!ctx) {
      const AC: typeof AudioContext =
        (window as unknown as { AudioContext?: typeof AudioContext; webkitAudioContext?: typeof AudioContext })
          .AudioContext ||
        (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext!;
      if (!AC) return null;
      ctx = new AC();
    }
    if (ctx.state === "suspended") void ctx.resume().catch(() => {});
    return ctx;
  } catch {
    return null;
  }
};

const beep = (ac: AudioContext, freq: number, start: number, dur: number, gain: number) => {
  const osc = ac.createOscillator();
  const g = ac.createGain();
  osc.type = "sine";
  osc.frequency.value = freq;
  g.gain.setValueAtTime(0.0001, ac.currentTime + start);
  g.gain.exponentialRampToValueAtTime(gain, ac.currentTime + start + 0.02);
  g.gain.exponentialRampToValueAtTime(0.0001, ac.currentTime + start + dur);
  osc.connect(g).connect(ac.destination);
  osc.start(ac.currentTime + start);
  osc.stop(ac.currentTime + start + dur + 0.02);
};

export const playNotificationSound = (): void => {
  const now = Date.now();
  if (now - lastPlayed < THROTTLE_MS) return;
  lastPlayed = now;
  const ac = getCtx();
  if (!ac) return;
  try {
    beep(ac, 880, 0, 0.14, 0.18); // A5
    beep(ac, 1320, 0.12, 0.16, 0.14); // E6
  } catch {
    /* ignore */
  }
};