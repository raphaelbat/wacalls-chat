import { useMutation } from "@tanstack/react-query";
import { rejectCall } from "@/services/calls";
import { clearIncoming } from "@/stores/calls";

export const useRejectCall = () =>
  useMutation({
    mutationFn: async (vars: { sid: string; callId: string }) => {
      // Fecha o popup imediatamente para dar feedback instantâneo.
      clearIncoming();
      try {
        await rejectCall(vars.sid, vars.callId);
      } catch {}
    },
    onSettled: () => clearIncoming(),
  });
