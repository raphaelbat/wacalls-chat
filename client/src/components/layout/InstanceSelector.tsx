import { useEffect, useMemo } from "react";
import { Check, ChevronDown, Wifi, WifiOff } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ensureSessionsWired, setActiveSession, useSessions } from "@/stores/sessions";
import { useNavigate } from "react-router-dom";
import type { SessionInfo } from "@/types/session";

const stateLabel = (s: SessionInfo): { text: string; tone: "ok" | "warn" | "off" } => {
  if (s.paired && s.state === "open") return { text: "Conectado", tone: "ok" };
  if (s.state === "qr" || s.state === "connecting") return { text: "Aguardando", tone: "warn" };
  return { text: "Desconectado", tone: "off" };
};

const dotClass = (tone: "ok" | "warn" | "off") =>
  tone === "ok"
    ? "bg-emerald-500"
    : tone === "warn"
      ? "bg-amber-500"
      : "bg-muted-foreground/40";

// Header-level instance switcher: shows the currently active WhatsApp
// connection and lets the user jump between paired sessions without
// leaving the page they are on. Reflects live connection state from SSE.
export const InstanceSelector = () => {
  const nav = useNavigate();
  const sessions = useSessions((s) => s.sessions);
  const activeId = useSessions((s) => s.activeId);
  useEffect(() => { ensureSessionsWired(); }, []);

  const active = useMemo(
    () => sessions.find((s) => s.id === activeId) ?? sessions[0],
    [sessions, activeId],
  );

  if (sessions.length === 0) {
    return (
      <button
        type="button"
        onClick={() => nav("/connections")}
        className="hidden h-9 items-center gap-2 rounded-full border border-dashed border-border/60 px-3 text-xs font-medium text-muted-foreground transition hover:bg-muted sm:flex"
      >
        <WifiOff className="h-3.5 w-3.5" /> Nenhuma instância
      </button>
    );
  }

  const meta = active ? stateLabel(active) : { text: "—", tone: "off" as const };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label="Selecionar instância"
          className="group flex h-9 items-center gap-2 rounded-full border border-border/60 bg-muted/40 px-2.5 transition hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
        >
          <span
            aria-hidden
            className="grid h-6 w-6 place-items-center rounded-full"
            style={active?.color ? { backgroundColor: active.color, color: "white" } : undefined}
          >
            <Wifi className="h-3 w-3" />
          </span>
          <span className="hidden min-w-0 flex-col leading-tight sm:flex">
            <span className="truncate text-[12px] font-semibold text-foreground">
              {active?.name ?? "Instância"}
            </span>
            <span className="flex items-center gap-1 text-[10px] text-muted-foreground">
              <span className={`h-1.5 w-1.5 rounded-full ${dotClass(meta.tone)}`} />
              {meta.text}
            </span>
          </span>
          <ChevronDown className="h-3.5 w-3.5 text-muted-foreground transition group-data-[state=open]:rotate-180" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-72">
        <DropdownMenuLabel className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
          Instâncias
        </DropdownMenuLabel>
        {sessions.map((s) => {
          const m = stateLabel(s);
          const isActive = s.id === active?.id;
          return (
            <DropdownMenuItem
              key={s.id}
              onSelect={() => setActiveSession(s.id)}
              className="flex items-center gap-2"
            >
              <span
                aria-hidden
                className="grid h-6 w-6 place-items-center rounded-full bg-primary/10 text-primary"
                style={s.color ? { backgroundColor: s.color, color: "white" } : undefined}
              >
                <Wifi className="h-3 w-3" />
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-medium">{s.name}</span>
                <span className="flex items-center gap-1 text-[10px] text-muted-foreground">
                  <span className={`h-1.5 w-1.5 rounded-full ${dotClass(m.tone)}`} />
                  {m.text}
                  {s.isDefault && <span className="ml-1 rounded bg-muted px-1 py-px text-[9px] uppercase">Padrão</span>}
                </span>
              </span>
              {isActive && <Check className="h-3.5 w-3.5 text-primary" />}
            </DropdownMenuItem>
          );
        })}
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => nav("/connections")} className="text-xs">
          Gerenciar conexões
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
};