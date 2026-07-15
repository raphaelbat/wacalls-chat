import { useEffect, useState } from "react";
import * as settingsApi from "@/services/settings";
import { subscribeWhitelabel, readCachedWhitelabel } from "@/lib/whitelabel";

/**
 * Splash estático (sem temporizadores) usado como `fallback` de
 * `<Suspense>` para cobrir carregamentos de rotas com lazy/chunks.
 * Permanece visível enquanto o Suspense estiver suspenso e é
 * desmontado automaticamente assim que o conteúdo da rota fica pronto.
 */
export const SplashFallback = () => {
  return (
    <div
      aria-hidden
      className="pointer-events-none fixed inset-0 z-[100] flex items-center justify-center bg-background"
    >
      <div className="splash-ring flex flex-col items-center gap-4">
        <div className="splash-ring__stage relative flex h-24 w-24 items-center justify-center">
          <span className="splash-ring__wave" />
          <span className="splash-ring__wave splash-ring__wave--delay" />
          <div className="splash-ring__badge relative flex h-24 w-24 items-center justify-center rounded-full border-2 border-border bg-card shadow-sm">
            <img src="/favicon.png" alt="" className="splash-ring__icon h-10 w-10 object-contain" />
          </div>
        </div>
        <div className="mt-2 flex gap-1">
          <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-primary" style={{ animationDelay: "0ms" }} />
          <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-primary" style={{ animationDelay: "120ms" }} />
          <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-primary" style={{ animationDelay: "240ms" }} />
        </div>
      </div>
    </div>
  );
};

/**
 * Splash de boas-vindas exibido apenas na inicialização do app.
 * Garante uma duração mínima (evita piscar quando o whitelabel carrega
 * rápido) e uma duração máxima (evita travar a UI se algo demorar).
 * Não re-dispara em troca de rota para não causar flicker em navegações
 * locais rápidas.
 */
export const Splash = ({
  minDurationMs = 700,
  maxDurationMs = 1800,
}: {
  minDurationMs?: number;
  maxDurationMs?: number;
} = {}) => {
  const [wl, setWl] = useState<settingsApi.Whitelabel | null>(
    () => (readCachedWhitelabel() as settingsApi.Whitelabel | null) ?? null,
  );
  const [visible, setVisible] = useState(true);
  const [fading, setFading] = useState(false);
  const [wlReady, setWlReady] = useState(false);
  const [minElapsed, setMinElapsed] = useState(false);

  useEffect(() => {
    let alive = true;
    settingsApi
      .getWhitelabel()
      .then((v) => {
        if (!alive) return;
        setWl((prev) => ({ ...(prev || {}), ...v } as settingsApi.Whitelabel));
      })
      .catch(() => {})
      .finally(() => {
        if (alive) setWlReady(true);
      });
    const off = subscribeWhitelabel((v) => setWl(v));
    return () => { alive = false; off(); };
  }, []);

  useEffect(() => {
    const tMin = window.setTimeout(() => setMinElapsed(true), minDurationMs);
    const tMax = window.setTimeout(() => {
      setFading(true);
      window.setTimeout(() => { setVisible(false); }, 350);
    }, maxDurationMs);
    return () => {
      window.clearTimeout(tMin);
      window.clearTimeout(tMax);
    };
  }, [minDurationMs, maxDurationMs]);

  // Quando whitelabel carregou e o tempo mínimo passou, encerra o splash.
  useEffect(() => {
    if (!visible || fading) return;
    if (wlReady && minElapsed) {
      setFading(true);
      const t = window.setTimeout(() => { setVisible(false); }, 350);
      return () => window.clearTimeout(t);
    }
  }, [wlReady, minElapsed, visible, fading]);

  if (!visible) return null;

  const name = wl?.appName || "VozZap";
  const isDark = typeof document !== "undefined" && document.documentElement.classList.contains("dark");
  const brandLogo = (isDark ? wl?.logoDark : wl?.logoLight) || wl?.logoLight || wl?.logoDark;

  return (
    <div
      aria-hidden
      className={`pointer-events-none fixed inset-0 z-[100] flex items-center justify-center bg-background transition-opacity duration-300 ${fading ? "opacity-0" : "opacity-100"}`}
    >
      <div className="flex flex-col items-center gap-4">
        <div className="flex h-28 w-28 items-center justify-center rounded-full border border-border bg-card shadow-sm animate-pulse">
          <img src={brandLogo || "/favicon.png"} alt={name} className="h-20 w-20 object-contain" />
        </div>
        <div className="mt-2 flex gap-1">
          <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-primary" style={{ animationDelay: "0ms" }} />
          <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-primary" style={{ animationDelay: "120ms" }} />
          <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-primary" style={{ animationDelay: "240ms" }} />
        </div>
      </div>
    </div>
  );
};