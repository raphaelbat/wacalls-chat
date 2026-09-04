import { create } from "zustand";
import { eventStream } from "@/lib/event-stream";

type HoldState = {
  onHold: Record<string, boolean>;
  setLocal: (callId: string, on: boolean) => void;
};

export const useHoldStore = create<HoldState>((set) => ({
  onHold: {},
  setLocal: (callId, on) =>
    set((s) => ({ onHold: { ...s.onHold, [callId]: on } })),
}));

let wired = false;
export const ensureHoldWired = () => {
  if (wired) return;
  wired = true;
  eventStream.on((ev) => {
    const anyEv = ev as unknown as { type: string; id?: string; on?: boolean };
    if (anyEv?.type !== "call-hold" || !anyEv.id) return;
    useHoldStore.getState().setLocal(anyEv.id, !!anyEv.on);
  });
};
