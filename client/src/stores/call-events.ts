import { create } from "zustand";
import { eventStream, type BrokerEvent } from "@/lib/event-stream";

export type CallEvent = {
  id: string;
  callId: string;
  sessionId: string;
  at: number;
  kind: "incoming" | "status" | "claimed" | "ended";
  label: string;
  detail?: string;
};

type State = { events: Record<string, CallEvent[]> };

export const useCallEvents = create<State>(() => ({ events: {} }));

const MAX_PER_CALL = 200;
let seq = 0;
const nextId = (): string => `${Date.now()}-${++seq}`;

const push = (callId: string, ev: CallEvent): void => {
  useCallEvents.setState((s) => {
    const list = s.events[callId] ?? [];
    const next = [...list, ev];
    if (next.length > MAX_PER_CALL) next.splice(0, next.length - MAX_PER_CALL);
    return { events: { ...s.events, [callId]: next } };
  });
};

let wired = false;
export const ensureCallEventsWired = (): void => {
  if (wired) return;
  wired = true;
  eventStream.on((ev: BrokerEvent) => {
    if (ev.type === "incoming") {
      push(ev.id, {
        id: nextId(),
        callId: ev.id,
        sessionId: ev.sessionId,
        at: ev.offeredAt || Date.now(),
        kind: "incoming",
        label: ev.video ? "Chamada de vídeo recebida" : "Chamada recebida",
        detail: ev.peer,
      });
    } else if (ev.type === "call-status") {
      push(ev.id, {
        id: nextId(),
        callId: ev.id,
        sessionId: ev.sessionId,
        at: ev.startedAt || Date.now(),
        kind: "status",
        label: `Status: ${ev.status}`,
        detail: ev.peer,
      });
    } else if (ev.type === "incoming-claimed") {
      push(ev.id, {
        id: nextId(),
        callId: ev.id,
        sessionId: ev.sessionId,
        at: Date.now(),
        kind: "claimed",
        label: "Atendida por outro agente",
        detail: ev.owner,
      });
    } else if (ev.type === "call-ended") {
      push(ev.id, {
        id: nextId(),
        callId: ev.id,
        sessionId: ev.sessionId,
        at: ev.endedAt || Date.now(),
        kind: "ended",
        label: "Chamada encerrada",
        detail: ev.reason,
      });
    }
  });
};