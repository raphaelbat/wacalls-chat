import { useEffect } from "react";
import { eventStream } from "@/lib/event-stream";
import { toast } from "sonner";
import { formatPhone } from "@/lib/phone-format";

// Global listener: whenever the SSE broker delivers a `call-transfer-request`
// targeted at this user, surface a toast so the agent can pick up.
export const useIncomingTransfer = () => {
  useEffect(() => {
    const off = eventStream.on((ev) => {
      // Loose check because the union type in event-stream.ts is narrower.
      const anyEv = ev as unknown as {
        type: string;
        peer?: string;
        fromName?: string;
        note?: string;
      };
      if (anyEv?.type !== "call-transfer-request") return;
      toast.success("Chamada transferida para você", {
        description: `${anyEv.fromName ?? "Atendente"} → ${anyEv.peer ? formatPhone(anyEv.peer) : "chamada"}${anyEv.note ? ` · ${anyEv.note}` : ""}`,
      });
    });
    return () => { off(); };
  }, []);
};
