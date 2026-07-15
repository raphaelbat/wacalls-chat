import { useEffect, useState } from "react";
import { Loader2, Smartphone, Users2 } from "lucide-react";
import { toast } from "sonner";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { updateSession } from "@/services/sessions";
import { listQueues } from "@/services/queues";
import type { SessionInfo } from "@/types/session";
import type { Queue } from "@/types/queue";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  session: SessionInfo;
  onSaved?: () => void;
};

export const EditConnectionModal = ({ open, onOpenChange, session, onSaved }: Props) => {
  const [allowGroups, setAllowGroups] = useState(!!session.allowGroups);
  const [queueId, setQueueId] = useState(session.queueId ?? "");
  const [queues, setQueues] = useState<Queue[]>([]);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!open) return;
    setAllowGroups(!!session.allowGroups);
    setQueueId(session.queueId ?? "");
    void listQueues().then(setQueues).catch(() => {});
  }, [open, session]);

  const onSave = async () => {
    setBusy(true);
    try {
      await updateSession(session.id, {
        name: session.name,
        color: session.color ?? "#57adf8",
        isDefault: !!session.isDefault,
        allowGroups,
        queueId,
        redirectMinutes: session.redirectMinutes ?? 0,
        flowId: session.flowId ?? "",
        greetingMessage: session.greetingMessage ?? "",
        completionMessage: session.completionMessage ?? "",
        outOfHoursMessage: session.outOfHoursMessage ?? "",
        surveyEnabled: !!session.surveyEnabled,
        surveyPrompt: session.surveyPrompt ?? "",
      });
      toast.success("Conexão atualizada");
      onSaved?.();
      onOpenChange(false);
    } catch (e) {
      toast.error((e as Error).message);
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
              <DialogTitle className="truncate text-base">Editar conexão</DialogTitle>
              <p className="truncate text-xs text-muted-foreground">{session.jid || "Aguardando pareamento"}</p>
            </div>
          </div>
        </DialogHeader>

        <div className="space-y-5 px-6 py-5">
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
        </div>

        <div className="flex justify-end gap-2 border-t bg-muted/20 px-6 py-3">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancelar
          </Button>
          <Button onClick={onSave} disabled={busy}>
            {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            Salvar alterações
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
};
