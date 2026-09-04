import { create } from "zustand";

// UI state for the global floating dialer (FAB-triggered draggable panel).
// Only the open/minimized state lives here — call-state stays in `useCalls`.
type DialerUI = {
  open: boolean;
  minimized: boolean;
  // Top-left position in viewport px. `null` means use default placement.
  pos: { x: number; y: number } | null;
  prefill: string | null;
  openDialer: (phone?: string) => void;
  close: () => void;
  toggleMinimize: () => void;
  setPos: (p: { x: number; y: number }) => void;
  clearPrefill: () => void;
};

export const useDialerUI = create<DialerUI>((set) => ({
  open: false,
  minimized: false,
  pos: null,
  prefill: null,
  openDialer: (phone) =>
    set({ open: true, minimized: false, prefill: phone ?? null }),
  close: () => set({ open: false, minimized: false }),
  toggleMinimize: () => set((s) => ({ minimized: !s.minimized })),
  setPos: (p) => set({ pos: p }),
  clearPrefill: () => set({ prefill: null }),
}));