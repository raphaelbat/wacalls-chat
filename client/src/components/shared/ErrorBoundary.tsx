import { Component, type ReactNode } from "react";

interface Props { children: ReactNode }
interface State { error: Error | null }

const RELOAD_KEY = "voipinho.eb.autoreload";
const MAX_AUTO_RELOADS = 3;

type ReloadState = { count: number; at: number };

function readReloadState(): ReloadState {
  try {
    const raw = sessionStorage.getItem(RELOAD_KEY);
    if (!raw) return { count: 0, at: 0 };
    const parsed = JSON.parse(raw) as ReloadState;
    // Reset window after 60s so transient issues don't permanently lock the UI.
    if (Date.now() - parsed.at > 60_000) return { count: 0, at: 0 };
    return parsed;
  } catch {
    return { count: 0, at: 0 };
  }
}

function writeReloadState(s: ReloadState): void {
  try { sessionStorage.setItem(RELOAD_KEY, JSON.stringify(s)); } catch { /* ignore */ }
}

// Clear the counter shortly after a healthy mount so the next transient
// error in a future navigation can also auto-recover.
if (typeof window !== "undefined") {
  window.setTimeout(() => {
    try { sessionStorage.removeItem(RELOAD_KEY); } catch { /* ignore */ }
  }, 8_000);
}

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: unknown) {
    // eslint-disable-next-line no-console
    console.error("[ErrorBoundary]", error, info);
    const msg = `${error?.name ?? ""}: ${error?.message ?? ""}`;
    const isTransient =
      /before initialization/i.test(msg) ||
      /is not defined/i.test(msg) ||
      /undefined is not an object/i.test(msg) ||
      /null is not an object/i.test(msg) ||
      /ChunkLoadError/i.test(msg) ||
      /Loading chunk/i.test(msg) ||
      /Failed to fetch dynamically imported module/i.test(msg) ||
      /Importing a module script failed/i.test(msg);
    if (isTransient) {
      const st = readReloadState();
      if (st.count < MAX_AUTO_RELOADS) {
        writeReloadState({ count: st.count + 1, at: Date.now() });
        // Tiny delay avoids tight reload loops if the error fires sync on mount.
        window.setTimeout(() => window.location.reload(), 150);
      }
    }
  }

  render() {
    if (!this.state.error) return this.props.children;
    // Nunca mostra stack trace ao usuário. Exibe apenas um splash silencioso
    // com a logo, enquanto tenta auto-recuperar via reload.
    const st = readReloadState();
    if (st.count === 0) {
      writeReloadState({ count: 1, at: Date.now() });
      window.setTimeout(() => window.location.reload(), 150);
    }
    return (
      <div className="grid min-h-screen place-items-center bg-background">
        <img src="/favicon.png" alt="" className="h-16 w-16 animate-pulse object-contain opacity-80" />
      </div>
    );
  }
}