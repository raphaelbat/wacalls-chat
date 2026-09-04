import { useEffect, useState } from "react";
import { Mic, MicOff, PhoneOff } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useCalls } from "@/stores/calls";
import { useSessions } from "@/stores/sessions";
import { useEndCall } from "@/hooks/useEndCall";

const formatPeer = (peer: string): string => {
  const digits = (peer || "").split("@")[0]?.replace(/\D+/g, "") ?? "";
  return digits ? `+${digits}` : peer;
};

const elapsed = (startedAt: number, now: number) => {
  const s = Math.max(0, Math.floor((now - startedAt) / 1000));
  const m = Math.floor(s / 60);
  return `${String(m).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}`;
};

/**
 * Barra flutuante global com as chamadas em andamento. Fica sempre visível
 * (independente da tela aberta) para que o operador consiga desligar a
 * ligação depois que o cliente atende — antes o botão de desligar só existia
 * dentro do chat e sumia quando a chamada mudava de estado.
 */
export const ActiveCallBar = () => {
  const calls = useCalls((s) => s.calls);
  const ownConnections = useCalls((s) => s.ownConnections);
  const sessions = useSessions((s) => s.sessions);
  const end = useEndCall();
  const [now, setNow] = useState(() => Date.now());
  const [muted, setMuted] = useState<Record<string, boolean>>({});

  useEffect(() => {
    if (calls.length === 0) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [calls.length]);

  if (calls.length === 0) return null;

  const toggleMute = (callId: string) => {
    const conn = ownConnections.get(callId);
    if (!conn) return;
    const next = !muted[callId];
    conn.micStream.getAudioTracks().forEach((t) => {
      t.enabled = !next;
    });
    setMuted((m) => ({ ...m, [callId]: next }));
  };

  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-4 z-50 flex flex-col items-center gap-2 px-4">
      {calls.map((c) => {
        const sessionName = sessions.find((s) => s.id === c.sessionId)?.name || c.sessionId;
        const label =
          c.status === "connected" ? "Em chamada" : c.status === "ringing" ? "Chamando…" : "Conectando…";
        const isMuted = !!muted[c.callId];
        const hasAudio = ownConnections.has(c.callId);
        return (
          <div
            key={c.callId}
            className="pointer-events-auto flex w-full max-w-md items-center gap-3 rounded-full border border-emerald-500/30 bg-card/95 px-4 py-2 shadow-lg backdrop-blur"
          >
            <span className="relative flex h-2 w-2 shrink-0">
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-500 opacity-75" />
              <span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-500" />
            </span>
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium">{formatPeer(c.peer)}</p>
              <p className="truncate text-[11px] text-muted-foreground">
                {label} · {sessionName} · {elapsed(c.startedAt, now)}
              </p>
            </div>
            {hasAudio && (
              <Button
                size="icon"
                variant="ghost"
                title={isMuted ? "Ativar microfone" : "Silenciar microfone"}
                onClick={() => toggleMute(c.callId)}
              >
                {isMuted ? <MicOff className="h-4 w-4 text-rose-500" /> : <Mic className="h-4 w-4" />}
              </Button>
            )}
            <Button
              size="sm"
              variant="destructive"
              className="rounded-full"
              title="Desligar chamada"
              disabled={end.isPending}
              onClick={() => end.mutate({ sid: c.sessionId, callId: c.callId })}
            >
              <PhoneOff className="h-4 w-4" />
            </Button>
          </div>
        );
      })}
    </div>
  );
};