import { useEffect, useState } from "react";
import { Loader2, Search, UserPlus, MessageSquarePlus, Users } from "lucide-react";
import { toast } from "sonner";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { listContacts, createContact, type ContactRow } from "@/services/contacts";
import { fetchChats, setActiveChat } from "@/stores/chats";
import { assignChatTo, listOperators, type OperatorRef } from "@/services/chats";
import { listQueues } from "@/services/queues";
import type { Queue } from "@/types/queue";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  sessionId: string;
  onOpened?: (chatJid: string) => void;
}

type Mode = "search" | "manual";

interface PendingTarget {
  sessionId: string;
  chatJid: string;
  label: string;
}

const onlyDigits = (s: string) => s.replace(/\D+/g, "");

export const NewChatDialog = ({ open, onOpenChange, sessionId, onOpened }: Props) => {
  const [mode, setMode] = useState<Mode>("search");
  const [q, setQ] = useState("");
  const [loading, setLoading] = useState(false);
  const [results, setResults] = useState<ContactRow[]>([]);
  const [phone, setPhone] = useState("");
  const [name, setName] = useState("");
  const [creating, setCreating] = useState(false);
  const [operators, setOperators] = useState<OperatorRef[]>([]);
  const [queues, setQueues] = useState<Queue[]>([]);
  const [assignUserId, setAssignUserId] = useState("");
  const [assignQueueId, setAssignQueueId] = useState("");

  useEffect(() => {
    if (!open) {
      setQ("");
      setResults([]);
      setPhone("");
      setName("");
      setMode("search");
      setAssignUserId("");
      setAssignQueueId("");
    }
  }, [open]);

  useEffect(() => {
    if (!open) return;
    void listOperators().then(setOperators).catch(() => setOperators([]));
    void listQueues().then(setQueues).catch(() => setQueues([]));
  }, [open]);

  // Debounced contact search
  useEffect(() => {
    if (!open || mode !== "search") return;
    let cancel = false;
    setLoading(true);
    const t = window.setTimeout(async () => {
      try {
        const r = await listContacts({ q: q.trim(), kind: "user", limit: 30 });
        if (!cancel) setResults(r.contacts ?? []);
      } catch {
        if (!cancel) setResults([]);
      } finally {
        if (!cancel) setLoading(false);
      }
    }, 220);
    return () => {
      cancel = true;
      window.clearTimeout(t);
    };
  }, [q, open, mode]);

  // openImmediately assigns the chosen operator/queue and opens the chat.
  // At least one of userId / queueId is required (validated by callers).
  const openImmediately = async (target: PendingTarget) => {
    try {
      await assignChatTo(target.sessionId, target.chatJid, {
        userId: assignUserId,
        queueId: assignQueueId,
      });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Falha ao atribuir atendimento");
      return;
    }
    setActiveChat(target.sessionId, target.chatJid);
    await fetchChats(target.sessionId).catch(() => {});
    toast.success("Atendimento aberto");
    onOpened?.(target.chatJid);
    onOpenChange(false);
  };

  const ensureAssignment = (): boolean => {
    if (!assignUserId && !assignQueueId) {
      toast.error("Selecione um operador ou uma fila para abrir o atendimento.");
      return false;
    }
    return true;
  };

  const handlePickContact = (c: ContactRow) => {
    if (!ensureAssignment()) return;
    void openImmediately({
      sessionId: c.sessionId || sessionId,
      chatJid: c.chatJid,
      label: c.name || c.phone || c.chatJid,
    });
  };

  const handleManual = async () => {
    const digits = onlyDigits(phone);
    if (digits.length < 8) {
      toast.error("Informe um número válido com DDI/DDD");
      return;
    }
    if (!sessionId) {
      toast.error("Selecione uma conexão antes de abrir um atendimento.");
      return;
    }
    if (!ensureAssignment()) return;
    setCreating(true);
    try {
      const contact = await createContact({
        sessionId,
        phone: digits,
        name: name.trim() || digits,
      });
      await openImmediately({
        sessionId: contact.sessionId || sessionId,
        chatJid: contact.chatJid,
        label: contact.name || contact.phone || digits,
      });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Falha ao abrir atendimento");
    } finally {
      setCreating(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Abrir atendimento</DialogTitle>
          <DialogDescription>
            Pesquise por um contato existente ou informe um número manualmente.
          </DialogDescription>
        </DialogHeader>

        <div className="flex gap-1 rounded-md border bg-muted/40 p-1 text-xs font-medium">
          <button
            type="button"
            onClick={() => setMode("search")}
            className={`flex flex-1 items-center justify-center gap-1.5 rounded-sm px-2 py-1.5 transition ${mode === "search" ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"}`}
          >
            <Search className="h-3.5 w-3.5" /> Pesquisar contato
          </button>
          <button
            type="button"
            onClick={() => setMode("manual")}
            className={`flex flex-1 items-center justify-center gap-1.5 rounded-sm px-2 py-1.5 transition ${mode === "manual" ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"}`}
          >
            <UserPlus className="h-3.5 w-3.5" /> Adicionar manualmente
          </button>
        </div>

        {mode === "search" ? (
          <div className="space-y-3">
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={q}
                onChange={(e) => setQ(e.target.value)}
                placeholder="Buscar por nome ou número…"
                className="pl-8"
                autoFocus
              />
            </div>
            <div className="max-h-72 overflow-y-auto rounded-md border">
              {loading ? (
                <div className="flex items-center justify-center gap-2 py-8 text-xs text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" /> Buscando…
                </div>
              ) : results.length === 0 ? (
                <div className="space-y-2 py-8 text-center text-xs text-muted-foreground">
                  <div>Nenhum contato encontrado.</div>
                  <Button variant="link" size="sm" onClick={() => setMode("manual")}>
                    Adicionar manualmente
                  </Button>
                </div>
              ) : (
                <ul className="divide-y">
                  {results.map((c) => (
                    <li key={`${c.sessionId}:${c.chatJid}`}>
                      <button
                        type="button"
                        onClick={() => handlePickContact(c)}
                        className="flex w-full items-center gap-3 px-3 py-2 text-left hover:bg-muted/60"
                      >
                        {c.avatarUrl ? (
                          <img src={c.avatarUrl} alt="" className="h-9 w-9 rounded-full object-cover" />
                        ) : (
                          <div className="grid h-9 w-9 place-items-center rounded-full bg-muted text-xs font-semibold uppercase">
                            {(c.name || c.phone || "?").slice(0, 2)}
                          </div>
                        )}
                        <div className="min-w-0 flex-1">
                          <div className="truncate text-sm font-medium">{c.name || c.phone}</div>
                          <div className="truncate text-[11px] text-muted-foreground">
                            {c.phone}
                            {c.sessionName ? ` · ${c.sessionName}` : ""}
                          </div>
                        </div>
                        <MessageSquarePlus className="h-4 w-4 text-muted-foreground" />
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </div>
        ) : (
          <div className="space-y-3">
            <div className="space-y-1">
              <label className="text-xs font-medium text-muted-foreground">Número (com DDI e DDD)</label>
              <Input
                value={phone}
                onChange={(e) => setPhone(e.target.value)}
                placeholder="Ex: 5511999999999"
                inputMode="tel"
                autoFocus
              />
            </div>
            <div className="space-y-1">
              <label className="text-xs font-medium text-muted-foreground">Nome (opcional)</label>
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Como salvar este contato"
              />
            </div>
            <div className="flex justify-end gap-2 pt-2">
              <Button variant="outline" onClick={() => onOpenChange(false)} disabled={creating}>
                Cancelar
              </Button>
              <Button onClick={handleManual} disabled={creating || !phone.trim()}>
                {creating ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <MessageSquarePlus className="mr-2 h-4 w-4" />}
                Continuar
              </Button>
            </div>
          </div>
        )}

        <div className="grid grid-cols-1 gap-2 rounded-md border bg-muted/30 p-2 sm:grid-cols-2">
          <div className="space-y-1">
            <label className="flex items-center gap-1.5 text-[11px] font-medium text-muted-foreground">
              <UserPlus className="h-3 w-3" /> Operador
            </label>
            <select
              value={assignUserId}
              onChange={(e) => setAssignUserId(e.target.value)}
              className="flex h-8 w-full rounded-md border border-input bg-background px-2 text-xs shadow-sm focus:outline-none focus:ring-2 focus:ring-ring"
            >
              <option value="">— Selecionar —</option>
              {operators.map((o) => (
                <option key={o.id} value={o.id}>
                  {o.name?.trim() || o.email}
                </option>
              ))}
            </select>
          </div>
          <div className="space-y-1">
            <label className="flex items-center gap-1.5 text-[11px] font-medium text-muted-foreground">
              <Users className="h-3 w-3" /> Fila
            </label>
            <select
              value={assignQueueId}
              onChange={(e) => setAssignQueueId(e.target.value)}
              className="flex h-8 w-full rounded-md border border-input bg-background px-2 text-xs shadow-sm focus:outline-none focus:ring-2 focus:ring-ring"
            >
              <option value="">— Selecionar —</option>
              {queues.map((qq) => (
                <option key={qq.id} value={qq.id}>
                  {qq.name}
                </option>
              ))}
            </select>
          </div>
          <p className="col-span-full text-[10px] text-muted-foreground">
            Selecione ao menos um operador ou uma fila para abrir o atendimento.
          </p>
        </div>
      </DialogContent>
    </Dialog>
  );
};