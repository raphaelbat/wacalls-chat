import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";

type State = {
  micId: string | null;
  outId: string | null;
  setMic: (id: string) => void;
  setOut: (id: string) => void;
};

export const useDevices = create<State>()(
  persist(
    (set) => ({
      micId: null,
      outId: null,
      setMic: (id) => set({ micId: id || null }),
      setOut: (id) => set({ outId: id || null }),
    }),
    {
      name: "prime-voip-devices",
      storage: createJSONStorage(() => localStorage),
    },
  ),
);

/**
 * Picks system defaults for mic/speaker on app start so that incoming calls
 * (atendimento) use the same devices as the dialer even when the operator
 * never opened the dialer to choose them manually.
 *
 * - Requests mic permission once so device labels become available.
 * - Sets the first audioinput / audiooutput as the default when no preference
 *   has been persisted yet (or when the persisted device is no longer present).
 */
export const initDefaultDevices = async () => {
  if (typeof navigator === "undefined" || !navigator.mediaDevices?.enumerateDevices) return;
  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    stream.getTracks().forEach((t) => t.stop());
  } catch {
    // Permission denied — getUserMedia will retry at call time.
  }
  let list: MediaDeviceInfo[] = [];
  try {
    list = await navigator.mediaDevices.enumerateDevices();
  } catch {
    return;
  }
  const inputs = list.filter((d) => d.kind === "audioinput" && d.deviceId);
  const outputs = list.filter((d) => d.kind === "audiooutput" && d.deviceId);
  const pickDefault = (devs: MediaDeviceInfo[]) => {
    if (!devs.length) return null;
    const tagged = devs.find((d) => d.deviceId === "default");
    return (tagged ?? devs[0]).deviceId || null;
  };
  const state = useDevices.getState();
  const micValid = state.micId && inputs.some((d) => d.deviceId === state.micId);
  const outValid = state.outId && outputs.some((d) => d.deviceId === state.outId);
  const next: Partial<Pick<State, "micId" | "outId">> = {};
  if (!micValid) next.micId = pickDefault(inputs);
  if (!outValid) next.outId = pickDefault(outputs);
  if (next.micId !== undefined || next.outId !== undefined) {
    useDevices.setState(next);
  }
};
