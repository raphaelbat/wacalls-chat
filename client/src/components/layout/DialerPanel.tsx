import { useEffect, useMemo, useState } from "react";
import { Phone, PhoneOff, X, Delete, Users, History, Search, Loader2 } from "lucide-react";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useDialerUI } from "@/stores/dialerUI";
import { useSessions } from "@/stores/sessions";
import { useStartCall } from "@/hooks/useStartCall";
import { useDevices } from "@/stores/devices";
import { toast } from "sonner";
import { listContacts, type ContactRow } from "@/services/contacts";
import { fetchCallHistory, type CallHistoryRow } from "@/services/callsHistory";

type Tab = "keypad" | "contacts" | "history";

const jidToPhone = (jid: string) => (jid || "").replace(/@.*/, "").replace(/\D/g, "");

const formatPhone = (raw: string) => {
  const d = (raw || "").replace(/\D/g, "");
  if (d.length >= 12) return `+${d.slice(0, 2)} (${d.slice(2, 4)}) ${d.slice(4, 9)}-${d.slice(9)}`;
  if (d.length === 11) return `(${d.slice(0, 2)}) ${d.slice(2, 7)}-${d.slice(7)}`;
  return raw;
};

const timeAgo = (ts: number) => {
  const s = Math.floor((Date.now() - ts) / 1000);
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}min`;
  if (s < 86400) return `${Math.floor(s / 3600)}h`;
  return `${Math.floor(s / 86400)}d`;
};

// Global floating dialer. Mounted once in AppShell. Opens whenever
// `useDialerUI.openDialer()` is called (e.g. header phone button).
export const DialerPanel = () => {
  const open = useDialerUI((s) => s.open);
  const close = useDialerUI((s) => s.close);
  const prefill = useDialerUI((s) => s.prefill);
  const clearPrefill = useDialerUI((s) => s.clearPrefill);

  const sessions = useSessions((s) => s.sessions);
  const activeId = useSessions((s) => s.activeId);

  const [tab, setTab] = useState<Tab>("keypad");
  const [sessionId, setSessionId] = useState<string>("");
  const [phone, setPhone] = useState<string>("");
  const [contactQuery, setContactQuery] = useState("");
  const [contacts, setContacts] = useState<ContactRow[]>([]);
  const [contactsLoading, setContactsLoading] = useState(false);
  const [history, setHistory] = useState<CallHistoryRow[]>([]);
  const [historyLoading, setHistoryLoading] = useState(false);
  const micId = useDevices((s) => s.micId);
  const outId = useDevices((s) => s.outId);

  useEffect(() => {
    if (!open) return;
    if (prefill) {
      setPhone(prefill);
      clearPrefill();
      setTab("keypad");
    }
    const preferred = activeId || sessions[0]?.id || "";
    setSessionId((prev) => prev || preferred);
  }, [open, prefill, clearPrefill, activeId, sessions]);

  // Load contacts when the tab opens or the search changes (debounced).
  useEffect(() => {
    if (!open || tab !== "contacts") return;
    let cancelled = false;
    setContactsLoading(true);
    const t = setTimeout(() => {
      listContacts({ q: contactQuery, kind: "user", limit: 50 })
        .then((res) => {
          if (!cancelled) setContacts(res.contacts);
        })
        .catch(() => {
          if (!cancelled) setContacts([]);
        })
        .finally(() => {
          if (!cancelled) setContactsLoading(false);
        });
    }, 250);
    return () => {
      cancelled = true;
      clearTimeout(t);
    };
  }, [open, tab, contactQuery]);

  // Load history when the tab opens.
  useEffect(() => {
    if (!open || tab !== "history") return;
    let cancelled = false;
    setHistoryLoading(true);
    fetchCallHistory({ limit: 50 })
      .then((res) => {
        if (!cancelled) setHistory(res.rows);
      })
      .catch(() => {
        if (!cancelled) setHistory([]);
      })
      .finally(() => {
        if (!cancelled) setHistoryLoading(false);
      });
  }, [open, tab]);

  const startCall = useStartCall(sessionId, micId, outId);

  const appendDigit = (d: string) => setPhone((p) => (p + d).slice(0, 20));
  const backspace = () => setPhone((p) => p.slice(0, -1));

  const digits = useMemo(() => phone.replace(/\D/g, ""), [phone]);
  const canCall = !!sessionId && digits.length >= 8 && !startCall.isPending;

  const handleCall = (overridePhone?: string, overrideSessionId?: string) => {
    const d = (overridePhone ?? digits).replace(/\D/g, "");
    const sid = overrideSessionId || sessionId;
    if (!sid || d.length < 8 || startCall.isPending) return;
    if (overrideSessionId && overrideSessionId !== sessionId) {
      setSessionId(overrideSessionId);
    }
    // useStartCall closes over sessionId at hook init; call directly via mutate
    startCall.mutate(
      { phone: d, record: false, video: false },
      {
        onSuccess: () => {
          toast.success("Chamando...");
          close();
        },
      },
    );
  };

  const callFromContact = (c: ContactRow) => {
    const p = c.phone || jidToPhone(c.chatJid);
    if (!p) {
      toast.error("Contato sem telefone");
      return;
    }
    setPhone(p);
    setTab("keypad");
    // Prefer the session that owns this contact
    handleCall(p, c.sessionId);
  };

  const callFromHistory = (r: CallHistoryRow) => {
    const p = r.phone || jidToPhone(r.peer);
    if (!p) {
      toast.error("Registro sem telefone");
      return;
    }
    setPhone(p);
    setTab("keypad");
    handleCall(p, r.sessionId);
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !v && close()}>
      <DialogContent showCloseButton={false} className="max-w-xs gap-0 p-0">
        <div className="flex items-center justify-between border-b px-4 py-3">
          <DialogTitle className="flex items-center gap-2 text-sm font-semibold">
            <Phone className="h-4 w-4 text-emerald-500" />
            Discador
          </DialogTitle>
          <button
            type="button"
            onClick={close}
            className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
            aria-label="Fechar"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Tabs */}
        <div className="grid grid-cols-3 border-b bg-muted/20 text-xs">
          {(
            [
              { id: "keypad", label: "Teclado", icon: Phone },
              { id: "contacts", label: "Contatos", icon: Users },
              { id: "history", label: "Histórico", icon: History },
            ] as { id: Tab; label: string; icon: typeof Phone }[]
          ).map((t) => {
            const Icon = t.icon;
            const active = tab === t.id;
            return (
              <button
                key={t.id}
                type="button"
                onClick={() => setTab(t.id)}
                className={`flex items-center justify-center gap-1.5 py-2 transition ${
                  active
                    ? "border-b-2 border-emerald-500 font-medium text-emerald-500"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                <Icon className="h-3.5 w-3.5" />
                {t.label}
              </button>
            );
          })}
        </div>

        <div className="space-y-4 p-4">
          <div className="space-y-1.5">
            <Label className="text-xs">Conexão</Label>
            <Select value={sessionId} onValueChange={setSessionId}>
              <SelectTrigger className="h-9">
                <SelectValue placeholder="Selecione uma conexão" />
              </SelectTrigger>
              <SelectContent>
                {sessions.length === 0 ? (
                  <div className="px-2 py-3 text-xs text-muted-foreground">
                    Nenhuma conexão disponível
                  </div>
                ) : (
                  sessions.map((s) => (
                    <SelectItem key={s.id} value={s.id}>
                      {s.name || s.id}
                    </SelectItem>
                  ))
                )}
              </SelectContent>
            </Select>
          </div>

          {tab === "keypad" && (
            <>
              <div className="space-y-1.5">
                <Label className="text-xs">Telefone (com DDI)</Label>
                <Input
                  inputMode="tel"
                  placeholder="5511999998888"
                  value={phone}
                  onChange={(e) => setPhone(e.target.value)}
                  className="h-11 text-center text-lg font-mono tracking-wider"
                />
              </div>

              <div className="grid grid-cols-3 gap-2">
                {["1", "2", "3", "4", "5", "6", "7", "8", "9", "*", "0", "#"].map((d) => (
                  <Button
                    key={d}
                    type="button"
                    variant="outline"
                    className="h-11 text-lg font-semibold"
                    onClick={() => appendDigit(d)}
                  >
                    {d}
                  </Button>
                ))}
              </div>

              <div className="flex items-center justify-between gap-2">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  onClick={backspace}
                  aria-label="Apagar"
                >
                  <Delete className="h-5 w-5" />
                </Button>
                <Button
                  type="button"
                  className="flex-1 h-11 bg-emerald-600 hover:bg-emerald-500 text-white"
                  onClick={() => handleCall()}
                  disabled={!canCall}
                >
                  <Phone className="mr-2 h-4 w-4" />
                  {startCall.isPending ? "Chamando..." : "Ligar"}
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  onClick={close}
                  aria-label="Cancelar"
                >
                  <PhoneOff className="h-5 w-5 text-rose-500" />
                </Button>
              </div>
            </>
          )}

          {tab === "contacts" && (
            <div className="space-y-2">
              <div className="relative">
                <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={contactQuery}
                  onChange={(e) => setContactQuery(e.target.value)}
                  placeholder="Buscar contato..."
                  className="h-9 pl-8 text-sm"
                />
              </div>
              <div className="max-h-72 overflow-y-auto rounded-md border">
                {contactsLoading ? (
                  <div className="flex items-center justify-center py-6 text-xs text-muted-foreground">
                    <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                    Carregando...
                  </div>
                ) : contacts.length === 0 ? (
                  <div className="py-6 text-center text-xs text-muted-foreground">
                    Nenhum contato encontrado
                  </div>
                ) : (
                  <ul className="divide-y">
                    {contacts.map((c) => {
                      const p = c.phone || jidToPhone(c.chatJid);
                      return (
                        <li
                          key={`${c.sessionId}:${c.chatJid}`}
                          className="flex items-center gap-2 px-2 py-2 hover:bg-muted/50"
                        >
                          {c.avatarUrl ? (
                            // eslint-disable-next-line @next/next/no-img-element
                            <img
                              src={c.avatarUrl}
                              alt=""
                              className="h-8 w-8 rounded-full object-cover"
                            />
                          ) : (
                            <span className="grid h-8 w-8 place-items-center rounded-full bg-muted text-[11px] font-medium">
                              {(c.name || p || "?").slice(0, 2).toUpperCase()}
                            </span>
                          )}
                          <div className="min-w-0 flex-1">
                            <div className="truncate text-sm font-medium">
                              {c.name || formatPhone(p)}
                            </div>
                            <div className="truncate text-[11px] text-muted-foreground">
                              {formatPhone(p)}
                            </div>
                          </div>
                          <Button
                            type="button"
                            size="icon"
                            variant="ghost"
                            aria-label="Ligar"
                            onClick={() => callFromContact(c)}
                            disabled={startCall.isPending}
                          >
                            <Phone className="h-4 w-4 text-emerald-500" />
                          </Button>
                        </li>
                      );
                    })}
                  </ul>
                )}
              </div>
            </div>
          )}

          {tab === "history" && (
            <div className="max-h-80 overflow-y-auto rounded-md border">
              {historyLoading ? (
                <div className="flex items-center justify-center py-6 text-xs text-muted-foreground">
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  Carregando...
                </div>
              ) : history.length === 0 ? (
                <div className="py-6 text-center text-xs text-muted-foreground">
                  Nenhuma ligação registrada
                </div>
              ) : (
                <ul className="divide-y">
                  {history.map((r) => {
                    const p = r.phone || jidToPhone(r.peer);
                    const isOut = r.direction === "outbound";
                    const missed = !r.answered && !isOut;
                    return (
                      <li
                        key={r.id}
                        className="flex items-center gap-2 px-2 py-2 hover:bg-muted/50"
                      >
                        <span
                          className={`grid h-7 w-7 place-items-center rounded-full ${
                            missed
                              ? "bg-rose-500/15 text-rose-500"
                              : isOut
                                ? "bg-emerald-500/15 text-emerald-500"
                                : "bg-sky-500/15 text-sky-500"
                          }`}
                        >
                          <Phone className="h-3.5 w-3.5" />
                        </span>
                        <div className="min-w-0 flex-1">
                          <div className="truncate text-sm font-medium">
                            {r.name || formatPhone(p)}
                          </div>
                          <div className="truncate text-[11px] text-muted-foreground">
                            {isOut ? "Saída" : missed ? "Perdida" : "Entrada"} · {timeAgo(r.startedAt)}
                          </div>
                        </div>
                        <Button
                          type="button"
                          size="icon"
                          variant="ghost"
                          aria-label="Ligar"
                          onClick={() => callFromHistory(r)}
                          disabled={startCall.isPending}
                        >
                          <Phone className="h-4 w-4 text-emerald-500" />
                        </Button>
                      </li>
                    );
                  })}
                </ul>
              )}
            </div>
          )}

          {sessions.length === 0 && (
            <p className="text-[11px] text-muted-foreground text-center">
              Conecte um número em Conexões para poder ligar.
            </p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
};
