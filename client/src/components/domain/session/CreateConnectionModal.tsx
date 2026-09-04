import { useEffect, useState } from "react";
import { Loader2, Smartphone, Users2 } from "lucide-react";
import { toast } from "sonner";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { createSession, updateSession } from "@/services/sessions";
import { useSessions } from "@/stores/sessions";
import { listQueues } from "@/services/queues";
import { ensureQuota } from "@/lib/quota";
import { toastError } from "@/lib/error-toast";
import type { Queue } from "@/types/queue";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated?: (id: string, tab?: string) => void;
};

// Open source build — same layout as EditConnectionModal so a new instance
// starts with the same fields the user later edits: descrição/nome, fila
// vinculada e recebimento de mensagens de grupo. Aplica-se à primeira
// conexão e a todas as demais (sem plano/pagamento, sem API oficial).
export const CreateConnectionModal = ({ open, onOpenChange, onCreated }: Props) => {
  const sessions = useSessions((s) => s.sessions);
  const sessionsCount = sessions.length;
  const [name, setName] = useState("");
  const [queueId, setQueueId] = useState("");
  const [allowGroups, setAllowGroups] = useState(false);
  const [queues, setQueues] = useState<Queue[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setName("");
    setQueueId("");
    setAllowGroups(false);
    setError(null);
    void listQueues().then(setQueues).catch(() => {});
  }, [open]);

  const onSubmit = async () => {
    const trimmed = name.trim();
    if (!trimmed) {
      setError("Informe a descrição da conexão");
      return;
    }
    if (!ensureQuota("conexoes", sessionsCount)) return;
    setBusy(true);
    try {
      const { id } = await createSession(trimmed, "free");
      try {
        await updateSession(id, {
          name: trimmed,
          color: "#57adf8",
          isDefault: false,
          allowGroups,
          queueId,
          redirectMinutes: 0,
          flowId: "",
        });
      } catch {
        /* tolerate — instance is created either way */
      }
      toast.success("Instância criada");
      onCreated?.(id);
      onOpenChange(false);
    } catch (e) {
      toastError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md overflow-hidden p-0">
        <DialogHeader className="border-b bg-gradient-to-r from-primary/10 via-primary/5 to-transparent px-6 py-5">
          <div className="flex items-center gap-3">
            <span className="grid h-11 w-11 place-items-center rounded-xl bg-primary/15 text-primary">
              <Smartphone className="h-5 w-5" />
            </span>
            <div className="min-w-0">
              <DialogTitle className="truncate text-base">Nova conexão</DialogTitle>
              <p className="truncate text-xs text-muted-foreground">
                Configure a descrição, fila e preferências de grupo.
              </p>
            </div>
          </div>
        </DialogHeader>

        <form
          onSubmit={(e) => {
            e.preventDefault();
            void onSubmit();
          }}
          className="space-y-5 px-6 py-5"
        >
          <div>
            <Label htmlFor="instance-name">Descrição da conexão</Label>
            <Input
              id="instance-name"
              autoFocus
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                if (error) setError(null);
              }}
              placeholder="Ex: Comercial, Suporte, Cobrança…"
              aria-invalid={!!error}
              className="mt-1.5 h-10"
            />
            {error ? (
              <p className="mt-1 text-xs text-destructive">{error}</p>
            ) : (
              <p className="mt-1.5 text-xs text-muted-foreground">
                Nome de referência para identificar esta conexão.
              </p>
            )}
          </div>

          <div>
            <Label htmlFor="cqueue">Fila vinculada</Label>
            <select
              id="cqueue"
              value={queueId}
              onChange={(e) => setQueueId(e.target.value)}
              className="h-10 w-full rounded-md border bg-background px-3 text-sm"
            >
              <option value="">— Sem fila —</option>
              {queues.map((q) => (
                <option key={q.id} value={q.id}>
                  {q.name}
                </option>
              ))}
            </select>
            <p className="mt-1.5 text-xs text-muted-foreground">
              Novas conversas são direcionadas para esta fila.
            </p>
          </div>

          <div className="flex items-center justify-between gap-3 rounded-lg border bg-card p-3">
            <div className="flex items-start gap-3">
              <span className="mt-0.5 grid h-8 w-8 place-items-center rounded-md bg-primary/10 text-primary">
                <Users2 className="h-4 w-4" />
              </span>
              <div>
                <div className="text-sm font-medium">Receber mensagens de grupo</div>
                <div className="text-xs text-muted-foreground">
                  Quando desativado, mensagens de grupos são ignoradas.
                </div>
              </div>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={allowGroups}
              onClick={() => setAllowGroups((v) => !v)}
              className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition ${
                allowGroups ? "bg-primary" : "bg-muted"
              }`}
            >
              <span
                className={`inline-block h-5 w-5 transform rounded-full bg-white shadow transition ${
                  allowGroups ? "translate-x-5" : "translate-x-0.5"
                }`}
              />
            </button>
          </div>

          <div className="flex justify-end gap-2 border-t pt-4">
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={busy}
            >
              Cancelar
            </Button>
            <Button type="submit" disabled={busy}>
              {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
              Criar conexão
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
};
