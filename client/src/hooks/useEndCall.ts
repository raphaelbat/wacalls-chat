import { useMutation } from "@tanstack/react-query";
import { endCall } from "@/services/calls";

export const useEndCall = () =>
  useMutation({
    mutationFn: async (vars: { sid: string; callId: string }) => {
      try {
        await endCall(vars.sid, vars.callId);
      } catch {}
    },
  });
