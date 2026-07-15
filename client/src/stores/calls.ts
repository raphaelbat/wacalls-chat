import { create } from "zustand";
import { toast } from "sonner";
import { eventStream, type BrokerEvent } from "@/lib/event-stream";
import { getClientId } from "@/lib/client-id";
import { queryClient, queryKeys } from "@/lib/query";
import type { OpenCall } from "@/lib/webrtc";
import type { CallSummary, IncomingPayload } from "@/types/call";

type State = {
  calls: CallSummary[];
  ownConnections: Map<string, OpenCall>;
  incoming: IncomingPayload | null;
};

export const useCalls = create<State>(() => ({
  calls: [],
  ownConnections: new Map(),
  incoming: null,
}));

export const resetCallsStore = (): void => {
  const current = useCalls.getState();
  for (const conn of current.ownConnections.values()) conn.close();
  useCalls.setState({ calls: [], ownConnections: new Map(), incoming: null });
};

let wired = false;
export const ensureCallsWired = (): void => {
  if (wired) return;
  wired = true;
  eventStream.on((ev: BrokerEvent) => {
    if (ev.type === "call-list") {
      useCalls.setState({ calls: ev.calls });
    } else if (ev.type === "call-status") {
      useCalls.setState((s) => ({
        calls: s.calls.map((c) =>
          c.callId === ev.id
            ? { ...c, sessionId: ev.sessionId, status: ev.status, peer: ev.peer, startedAt: ev.startedAt }
            : c,
        ),
      }));
    } else if (ev.type === "call-ended") {
      useCalls.setState((s) => {
        const conn = s.ownConnections.get(ev.id);
        if (conn) conn.close();
        const msg = conn ? callEndMessage(ev.reason) : null;
        if (msg) toast.error(msg);
        const next = new Map(s.ownConnections);
        next.delete(ev.id);
        return {
          calls: s.calls.filter((c) => c.callId !== ev.id),
          ownConnections: next,
          incoming: s.incoming?.callId === ev.id ? null : s.incoming,
        };
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.history });
    } else if (ev.type === "incoming") {
      useCalls.setState((s) => ({
        incoming: {
          sessionId: ev.sessionId,
          callId: ev.id,
          peer: ev.peer,
          peerName: ev.peerName || s.incoming?.peerName,
          video: ev.video,
          offeredAt: ev.offeredAt,
        },
      }));
    } else if (ev.type === "incoming-claimed") {
      useCalls.setState((s) => (s.incoming?.callId === ev.id ? { incoming: null } : s));
    } else if (ev.type === "ura-auto-attend") {
      // A URA atendeu automaticamente — não mostramos o modal "Incoming",
      // mas avisamos o operador com um toast persistente contendo
      // o número de origem e o horário em que a chamada chegou.
      const when = new Date(ev.ts || Date.now()).toLocaleTimeString("pt-BR", {
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
      });
      const who = ev.peerName?.trim() || formatPeer(ev.peer);
      toast.info(`URA assumiu o atendimento`, {
        description: `${ev.video ? "Vídeo" : "Voz"} • ${who} • ${when}`,
        duration: 8000,
        id: `ura-auto-${ev.id}`,
      });
    } else if (ev.type === "flow-skip") {
      // O backend pode reportar "flow-skip" mesmo quando a chamada disparou
      // normalmente por outro caminho (ex.: campanha sem URA inbound).
      // Para não poluir a UI com um toast vermelho enganoso, apenas logamos
      // no console — o operador ainda consegue puxar o trace se precisar.
      // eslint-disable-next-line no-console
      console.warn(
        `[flow-skip] traceId=${ev.traceId || "-"} reason=${ev.reason} callId=${ev.callId} detail=${ev.detail || "-"} → GET /api/flows/trace?callId=${ev.callId}`,
        ev,
      );
    }
  });
};

export const isMine = (call: CallSummary): boolean => call.owner === getClientId();

const callEndMessage = (reason: string): string | null => {
  if (reason === "timeout") return "Não foi possível estabelecer a mídia da chamada. Tente novamente.";
  if (reason === "aborted-before-sdp") return "A chamada foi encerrada antes de iniciar.";
  if (reason === "failed") return "A chamada falhou antes de conectar.";
  if (reason === "busy")
    return "O WhatsApp do destinatário recusou a chamada (ocupado ou sem permissão). Envie uma mensagem primeiro para liberar chamadas deste contato.";
  return null;
};

export const registerOwnConnection = (id: string, conn: OpenCall): void => {
  useCalls.setState((s) => {
    const next = new Map(s.ownConnections);
    next.set(id, conn);
    return { ownConnections: next };
  });
};

export const clearIncoming = (): void => useCalls.setState({ incoming: null });

const formatPeer = (peer: string): string => {
  // peer é tipicamente "<digits>@s.whatsapp.net" ou "<digits>@lid".
  const raw = (peer || "").split("@")[0] || peer;
  const digits = raw.replace(/\D+/g, "");
  if (digits.length >= 12 && digits.startsWith("55")) {
    const cc = digits.slice(0, 2);
    const ddd = digits.slice(2, 4);
    const rest = digits.slice(4);
    const mid = rest.length > 4 ? rest.slice(0, rest.length - 4) : rest;
    const end = rest.slice(-4);
    return `+${cc} (${ddd}) ${mid}-${end}`;
  }
  return digits ? `+${digits}` : peer;
};

const _flowSkipMessage = (reason: string, detail: string): string => {
  switch (reason) {
    case "no_inbound_flow":
      return "URA não disparou: nenhum fluxo habilitado com gatilho \"inbound\". Vincule um fluxo na conexão ou marque-o como inbound.";
    case "flow_disabled":
      return `URA não disparou: ${detail || "habilite o fluxo no FlowBuilder."}`;
    case "flow_not_found":
      return "URA não disparou: o fluxo vinculado foi removido. Revincule um fluxo na conexão.";
    case "flow_lookup_failed":
      return `URA não disparou: erro ao carregar fluxo (${detail}).`;
    case "tts_not_configured":
      return "URA disparou mas o TTS não está configurado (defina WACALLS_TTS_URL ou use voz ElevenLabs no nó).";
    case "tts_failed":
      return `URA: falha ao sintetizar voz (${detail}).`;
    default:
      return detail ? `URA: ${detail}` : "URA não disparou.";
  }
};
