import { create } from "zustand";
import * as settingsApi from "@/services/settings";
import type { Plan } from "@/services/settings";

// Catálogo de recursos -> usado para resolver hasFeature contra o plano ativo.
// As chaves precisam bater EXATAMENTE com os rótulos usados em
// SettingsPage > RECURSOS_GRUPOS.
export type FeatureKey = string;

type PlanState = {
  loaded: boolean;
  loading: boolean;
  plan: Plan | null;
  load: () => Promise<void>;
  reset: () => void;
};

export const usePlanStore = create<PlanState>((set, get) => ({
  loaded: false,
  loading: false,
  plan: null,
  load: async () => {
    if (get().loading || get().loaded) return;
    set({ loading: true });
    try {
      const data = await settingsApi.getActivePlan();
      // Se o plano está inativo, trate como ausente (todas features liberadas
      // por fallback — evita travar o sistema enquanto admin não escolhe).
      const p = data.plan && data.plan.ativo !== false ? data.plan : null;
      set({ plan: p, loaded: true, loading: false });
    } catch {
      set({ plan: null, loaded: true, loading: false });
    }
  },
  reset: () => set({ loaded: false, plan: null }),
}));

// Quando nenhum plano ativo está definido tudo é liberado (sistema "aberto").
// Quando há plano, apenas recursos marcados como true são permitidos.
export const hasFeature = (plan: Plan | null, feature: FeatureKey): boolean => {
  if (!plan) return true;
  const rec = plan.recursos || {};
  return !!rec[feature];
};

export const planLimit = (
  plan: Plan | null,
  kind: "usuarios" | "conexoes" | "filas",
): number => {
  if (!plan) return Number.POSITIVE_INFINITY;
  const v = Number(plan[kind] || 0);
  // 0 = ilimitado por convenção do formulário.
  return v <= 0 ? Number.POSITIVE_INFINITY : v;
};

export const usePlan = () => {
  const plan = usePlanStore((s) => s.plan);
  return {
    plan,
    hasFeature: (f: FeatureKey) => hasFeature(plan, f),
    limit: (k: "usuarios" | "conexoes" | "filas") => planLimit(plan, k),
  };
};