import { useEffect } from "react";
import { toast } from "sonner";
import { getFreeTier, type FreeTierAlert } from "@/services/billing";
import { useAuth } from "@/stores/auth";

const KIND_LABEL: Record<string, string> = {
  free_calls: "ligações semanais",
  free_chats: "conversas semanais",
  free_connections: "conexões",
};

const STORAGE_KEY = "vozzap.freeTierAlerts.seen";
const POLL_MS = 60_000;

function loadSeen(): Set<string> {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return new Set();
    const arr = JSON.parse(raw);
    if (!Array.isArray(arr)) return new Set();
    return new Set(arr.map(String));
  } catch {
    return new Set();
  }
}

function saveSeen(seen: Set<string>) {
  try {
    // Mantém só 50 chaves para não crescer sem limite.
    const arr = Array.from(seen).slice(-50);
    localStorage.setItem(STORAGE_KEY, JSON.stringify(arr));
  } catch {
    /* ignore */
  }
}

function alertKey(a: FreeTierAlert): string {
  return `${a.week}:${a.kind}:${a.threshold}`;
}

function emitToast(a: FreeTierAlert) {
  const label = KIND_LABEL[a.kind] ?? a.kind;
  if (a.threshold >= 100) {
    toast.error(`Limite gratuito atingido — ${label}`, {
      description:
        "Novas operações deste tipo serão bloqueadas. Assine um plano em Financeiro para liberar uso ilimitado.",
      duration: 8000,
    });
  } else {
    toast.warning(`Você atingiu ${a.threshold}% do limite gratuito de ${label}`, {
      description:
        "Considere contratar um plano em Financeiro para evitar interrupções nesta semana.",
      duration: 7000,
    });
  }
}

/**
 * Polla o status do plano gratuito e dispara toasts quando o backend reporta
 * alertas novos (80% / 100%). Usa localStorage para evitar repetir o mesmo
 * alerta entre recarregamentos — a dedupe definitiva (uma vez por semana, por
 * limiar e por tipo) já é feita no backend.
 */
export function useFreeTierAlerts() {
  const user = useAuth((s) => s.user);
  useEffect(() => {
    if (!user) return;
    let cancelled = false;
    const tick = async () => {
      try {
        const status = await getFreeTier();
        if (cancelled) return;
        if (status.paid) return;
        const seen = loadSeen();
        let changed = false;
        for (const a of status.alerts ?? []) {
          const key = alertKey(a);
          if (seen.has(key)) continue;
          emitToast(a);
          seen.add(key);
          changed = true;
        }
        if (changed) saveSeen(seen);
      } catch {
        /* ignore network errors */
      }
    };
    void tick();
    const id = window.setInterval(tick, POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, [user?.id]);
}