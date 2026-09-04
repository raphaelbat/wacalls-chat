import { create } from "zustand";
import * as settingsApi from "@/services/settings";
import type { Options } from "@/services/settings";

// Store leve para as opções globais (aba Opções nas Configurações).
// Permite que componentes como ChatList/ChatView leiam flags como
// `requireCloseReason` sem refazer o fetch a cada render.
type OptsState = {
  loaded: boolean;
  loading: boolean;
  options: Options;
  load: () => Promise<void>;
  set: (o: Options) => void;
  reset: () => void;
};

const DEFAULTS: Options = {};

export const useOptionsStore = create<OptsState>((set, get) => ({
  loaded: false,
  loading: false,
  options: DEFAULTS,
  load: async () => {
    if (get().loading || get().loaded) return;
    set({ loading: true });
    try {
      const data = await settingsApi.getOptions();
      set({ options: data || {}, loaded: true, loading: false });
    } catch {
      set({ loaded: true, loading: false });
    }
  },
  set: (o) => set({ options: o, loaded: true }),
  reset: () => set({ loaded: false, options: DEFAULTS }),
}));

export const useOption = <K extends keyof Options>(key: K): Options[K] =>
  useOptionsStore((s) => s.options[key]);