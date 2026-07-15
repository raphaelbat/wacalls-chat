import { useEffect, useMemo } from "react";
import { Mic, Phone, PhoneOff, User, Video, X } from "lucide-react";
import { useCalls, clearIncoming } from "@/stores/calls";
import { useDevices } from "@/stores/devices";
import { useAcceptCall } from "@/hooks/useAcceptCall";
import { useRejectCall } from "@/hooks/useRejectCall";
import { formatPhone } from "@/lib/phone-format";

/**
 * WhatsApp-style incoming call popup. Renders whenever the calls store has
 * an `incoming` payload so the operator can Accept/Reject the call from
 * inside the tool (previously it would only ring in the tray).
 * Visual reference: WhatsApp Desktop incoming call modal.
 */
export const IncomingCallModal = () => {
  const incoming = useCalls((s) => s.incoming);
  // clearIncoming is imported directly from the store module.
  const micId = useDevices((s) => s.micId);
  const outId = useDevices((s) => s.outId);
  const accept = useAcceptCall(micId, outId);
  const reject = useRejectCall();
  const ringing = !!incoming;

  const secondsRinging = useMemo(() => {
    if (!incoming) return 0;
    return Math.max(0, Math.floor((Date.now() - incoming.offeredAt) / 1000));
  }, [incoming]);

  // Synthesized ringtone (WhatsApp-like double-beep) via WebAudio.
  // We can't ship a real WAV without adding a binary asset, and browsers
  // block silent placeholders anyway, so we build the tone at runtime.
  useEffect(() => {
    if (!ringing) return;
    type WithWebkit = typeof window & { webkitAudioContext?: typeof AudioContext };
    const Ctor = window.AudioContext || (window as WithWebkit).webkitAudioContext;
    if (!Ctor) return;
    const ctx: AudioContext = new Ctor();
    let stopped = false;
    const beep = (freq: number, start: number, dur: number) => {
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = "sine";
      osc.frequency.value = freq;
      gain.gain.setValueAtTime(0, ctx.currentTime + start);
      gain.gain.linearRampToValueAtTime(0.25, ctx.currentTime + start + 0.02);
      gain.gain.linearRampToValueAtTime(0, ctx.currentTime + start + dur);
      osc.connect(gain).connect(ctx.destination);
      osc.start(ctx.currentTime + start);
      osc.stop(ctx.currentTime + start + dur + 0.05);
    };
    const playCycle = () => {
      if (stopped) return;
      // two short beeps, ~0.4s each, then 1.5s silence — WhatsApp cadence
      beep(880, 0, 0.35);
      beep(660, 0.45, 0.35);
    };
    playCycle();
    const interval = window.setInterval(playCycle, 2200);
    // Try to resume in case the context started suspended (autoplay policy).
    ctx.resume?.().catch(() => {});
    return () => {
      stopped = true;
      window.clearInterval(interval);
      try { ctx.close(); } catch { /* noop */ }
    };
  }, [ringing]);

  if (!incoming) return null;

  const phoneLabel = formatPhone(incoming.peer) || incoming.peer;
  const displayName = incoming.peerName?.trim() || "";
  const callTypeLabel = incoming.video ? "Video call" : "Voice call";

  const onAccept = () => {
    accept.mutate({ sid: incoming.sessionId, callId: incoming.callId, video: incoming.video });
  };
  const onReject = () => {
    reject.mutate({ sid: incoming.sessionId, callId: incoming.callId });
  };

  return (
    <div className="fixed inset-0 z-[100] flex items-center justify-center bg-black/60 backdrop-blur-sm p-4">
      <div className="w-[min(94vw,420px)] overflow-hidden rounded-2xl bg-neutral-900 text-white shadow-2xl ring-1 ring-white/10 animate-in fade-in zoom-in-95">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-white/10">
          <div className="flex items-center gap-2">
            <span className="grid h-6 w-6 place-items-center rounded-full bg-emerald-500">
              <Phone className="h-3.5 w-3.5 text-white" fill="currentColor" />
            </span>
            <span className="text-sm font-medium">WhatsApp</span>
          </div>
          <button
            type="button"
            aria-label="Fechar"
            onClick={() => clearIncoming()}
            className="rounded p-1 text-white/70 hover:bg-white/10 hover:text-white transition"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Body */}
        <div className="px-6 pt-8 pb-6 flex flex-col items-center text-center">
          <div className="mb-5 grid h-24 w-24 place-items-center rounded-full bg-neutral-700 text-neutral-300 ring-4 ring-emerald-500/20 animate-pulse">
            <User className="h-12 w-12" />
          </div>
          <div className="text-2xl font-semibold tracking-tight">{phoneLabel}</div>
          {displayName ? (
            <div className="mt-1 text-sm text-white/70">{displayName}</div>
          ) : null}
          <div className="mt-1 text-sm text-white/60 flex items-center gap-1.5">
            {incoming.video ? <Video className="h-4 w-4" /> : <Phone className="h-4 w-4" />}
            {callTypeLabel}
          </div>
          <div className="mt-1 text-[11px] text-white/40">tocando há {secondsRinging}s</div>

          {/* Mic indicator */}
          <div className="mt-6 grid h-11 w-11 place-items-center rounded-full bg-white/10 text-white/80">
            <Mic className="h-5 w-5" />
          </div>
        </div>

        {/* Actions */}
        <div className="px-6 pb-6 flex items-center justify-between gap-4">
          <button
            type="button"
            aria-label="Recusar chamada"
            onClick={onReject}
            disabled={reject.isPending}
            className="grid h-14 w-14 place-items-center rounded-full bg-red-600 text-white shadow-lg hover:bg-red-500 active:scale-95 transition disabled:opacity-60"
          >
            <PhoneOff className="h-6 w-6" />
          </button>

          <button
            type="button"
            aria-label="Atender chamada"
            onClick={onAccept}
            disabled={accept.isPending}
            className="flex-1 h-14 flex items-center justify-center gap-2 rounded-full bg-emerald-500 text-white font-semibold shadow-lg hover:bg-emerald-400 active:scale-[0.98] transition disabled:opacity-60"
          >
            <Phone className="h-5 w-5" fill="currentColor" />
            Atender
          </button>
        </div>
      </div>
    </div>
  );
};
