import type { CallStatus } from "@/types/call";
import type { SessionInfo, SessionState } from "@/types/session";
import type { ChatEvent, ChatMessage, ChatMeta } from "@/types/chat";

type CallListRow = {
  sessionId: string;
  callId: string;
  owner: string | null;
  direction: "outbound" | "inbound";
  peer: string;
  startedAt: number;
  status: CallStatus;
  endedAt?: number;
  endReason?: string;
};

export type BrokerEvent =
  | { type: "session-list"; sessions: SessionInfo[] }
  | { type: "session-qr"; sessionId: string; qr: string }
  | { type: "auth-state"; sessionId: string; paired: boolean; state: SessionState; qr?: string }
  | { type: "call-list"; calls: CallListRow[] }
  | { type: "call-status"; sessionId: string; id: string; owner: string | null; status: CallStatus; peer: string; startedAt: number }
  | { type: "call-ended"; sessionId: string; id: string; owner: string | null; reason: string; endedAt: number }
  | { type: "incoming"; sessionId: string; id: string; peer: string; peerName?: string; video: boolean; offeredAt: number }
  | { type: "incoming-claimed"; sessionId: string; id: string; owner: string }
  | { type: "ura-auto-attend"; sessionId: string; id: string; peer: string; peerName?: string; video: boolean; ts: number }
  | { type: "message"; sessionId: string; chatJid: string; message: ChatMessage }
  | { type: "chat-meta"; meta: ChatMeta }
  | { type: "chat-event"; event: ChatEvent }
  | { type: "flow-skip"; sessionId: string; callId: string; flowId: string; reason: string; detail: string; traceId?: string; ts: number }
  | { type: "billing-update"; userId: string; status: string; planId: string; currentPeriodEnd: number; ts: number }
  | { type: "call-hold"; sessionId: string; id: string; on: boolean; ts: number }
  | { type: "call-transfer-request"; sessionId: string; callId: string; peer: string; fromUserId: string; fromName: string; targetType: "user" | "queue"; targetId: string; note?: string; ts: number };

type Listener = (ev: BrokerEvent) => void;

class EventStream {
  #es: EventSource | null = null;
  #listeners = new Set<Listener>();

  connect(clientId: string): void {
    if (this.#es) return;
    this.#es = new EventSource(`/api/events?clientId=${encodeURIComponent(clientId)}`);
    this.#es.onmessage = (ev) => {
      try {
        const parsed: BrokerEvent = JSON.parse(ev.data);
        for (const l of this.#listeners) l(parsed);
      } catch {}
    };
    this.#es.onerror = () => {};
  }

  on(l: Listener): () => void {
    this.#listeners.add(l);
    return () => this.#listeners.delete(l);
  }

  close(): void {
    this.#es?.close();
    this.#es = null;
  }
}

export const eventStream = new EventStream();
