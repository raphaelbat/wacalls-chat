import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { listOperators, transferChat, type OperatorRef } from "@/services/chats";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  sessionId: string;
  chatJid: string;
  chatName: string;
  excludeUserId?: string | null;
  onTransferred?: () => void;
}

// TransferDialog lets an agent hand a ticket over to another operator.
// Searchable list of active users, scoped to the current authenticated session.
export const TransferDialog = ({ open, onOpenChange, sessionId, chatJid, chatName, excludeUserId, onTransferred }: Props) => {
  const [operators, setOperators] = useState<OperatorRef[]>([]);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [query, setQuery] = useState("");
  const [picked, setPicked] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setQuery("");
    setPicked(null);
    setLoading(true);
    listOperators()
      .then((ops) => setOperators(ops))
      .catch((err) => toast.error(err instanceof Error ? err.message : "Não foi possível carregar operadores"))
      .finally(() => setLoading(false));
  }, [open]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return operators
      .filter((o) => !excludeUserId || o.id !== excludeUserId)
      .filter((o) =>
        !q
          ? true
          : o.email.toLowerCase().includes(q) ||
            (o.name ?? "").toLowerCase().includes(q) ||
            (o.companyName ?? "").toLowerCase().includes(q),
      );
  }, [operators, query, excludeUserId]);

  const handleConfirm = async () => {
    if (!picked) return;
    setBusy(true);
    try {
      await transferChat(sessionId, chatJid, picked);
      toast.success("Atendimento transferido");
      onTransferred?.();
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Falha ao transferir");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Transferir atendimento</DialogTitle>
          <DialogDescription>Escolha o operador que vai assumir “{chatName}”.</DialogDescription>
        </DialogHeader>
        <Input
          autoFocus
          placeholder="Buscar por e-mail ou empresa…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <div className="scrollbar-thin max-h-72 overflow-y-auto rounded-md border">
          {loading ? (
            <div className="p-4 text-sm text-muted-foreground">Carregando…</div>
          ) : filtered.length === 0 ? (
            <div className="p-4 text-sm text-muted-foreground">Nenhum operador encontrado.</div>
          ) : (
            <ul>
              {filtered.map((op) => {
                const active = picked === op.id;
                return (
                  <li key={op.id}>
                    <button
                      type="button"
                      onClick={() => setPicked(op.id)}
                      className={`flex w-full items-center justify-between gap-3 border-b px-3 py-2 text-left text-sm last:border-b-0 hover:bg-muted/60 ${active ? "bg-muted" : ""}`}
                    >
                      <span className="min-w-0">
                        <span className="block truncate font-medium text-foreground">
                          {op.name?.trim() || op.email}
                        </span>
                        {(op.name?.trim() ? op.email : op.companyName) && (
                          <span className="block truncate text-xs text-muted-foreground">
                            {op.name?.trim() ? op.email : op.companyName}
                          </span>
                        )}
                      </span>
                      {active && (
                        <span className="rounded-full bg-primary px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-primary-foreground">
                          Selecionado
                        </span>
                      )}
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancelar
          </Button>
          <Button onClick={handleConfirm} disabled={!picked || busy}>
            Transferir
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};