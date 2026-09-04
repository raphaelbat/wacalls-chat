import { useEffect, useState } from "react";
import { Clock, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { toast } from "sonner";
import {
  cancelScheduledMessage,
  createScheduledMessage,
  listScheduledMessages,
  type ScheduledMessage,
} from "@/services/scheduledMessages";

interface Props {
  open: boolean;
  onClose: () => void;
  sessionId: string;
  chatJid: string;
  initialText?: string;
}

const toLocalInput = (ms: number) => {
  const d = new Date(ms - new Date().getTimezoneOffset() * 60000);
  return d.toISOString().slice(0, 16);
};

export const ScheduleDialog = ({ open, onClose, sessionId, chatJid, initialText }: Props) => {
  const [text, setText] = useState(initialText ?? "");
  const [when, setWhen] = useState(() => toLocalInput(Date.now() + 60 * 60 * 1000));
  const [followupHours, setFollowupHours] = useState("4");
  const [followupText, setFollowupText] = useState(
    "Olá! Ainda posso ajudar com o seu atendimento?",
  );
  const [items, setItems] = useState<ScheduledMessage[]>([]);
  const [busy, setBusy] = useState(false);

  const reload = async () => {
    try {
      setItems(await listScheduledMessages(sessionId, chatJid));
    } catch {
      setItems([]);
    }
  };

  useEffect(() => {
    if (!open) return;
    setText(initialText ?? "");
    void reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, sessionId, chatJid]);

  const submit = async (kind: "scheduled" | "followup") => {
    const body = kind === "scheduled" ? text.trim() : followupText.trim();
    if (!body) {
      toast.error("Escreva a mensagem.");
      return;
    }
    const runAt =
      kind === "scheduled"
        ? new Date(when).getTime()
        : Date.now() + Math.max(0.25, Number(followupHours) || 0) * 3600_000;
    if (!runAt || Number.isNaN(runAt)) {
      toast.error("Data/hora inválida.");
      return;
    }
    setBusy(true);
    try {
      await createScheduledMessage(sessionId, { chatJid, text: body, runAt, kind });
      toast.success(kind === "scheduled" ? "Mensagem agendada." : "Follow-up programado.");
      if (kind === "scheduled") setText("");
      await reload();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Falha ao agendar.");
    } finally {
      setBusy(false);
    }
  };

  const remove = async (id: string) => {
    try {
      await cancelScheduledMessage(sessionId, id);
      await reload();
    } catch {
      toast.error("Falha ao cancelar.");
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Clock className="h-4 w-4" /> Agendamento e follow-up
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-2 rounded-md border p-3">
            <Label>Mensagem agendada</Label>
            <Textarea rows={3} value={text} onChange={(e) => setText(e.target.value)} placeholder="Texto que será enviado" />
            <Input type="datetime-local" value={when} onChange={(e) => setWhen(e.target.value)} />
            <Button size="sm" disabled={busy} onClick={() => submit("scheduled")}>Agendar envio</Button>
          </div>

          <div className="space-y-2 rounded-md border p-3">
            <Label>Follow-up automático</Label>
            <p className="text-xs text-muted-foreground">
              Enviado só se o cliente não responder. Cancelado automaticamente na resposta.
            </p>
            <div className="flex items-center gap-2">
              <Input
                type="number"
                min="0.25"
                step="0.25"
                value={followupHours}
                onChange={(e) => setFollowupHours(e.target.value)}
                className="w-24"
              />
              <span className="text-sm text-muted-foreground">horas</span>
            </div>
            <Textarea rows={2} value={followupText} onChange={(e) => setFollowupText(e.target.value)} />
            <Button size="sm" variant="secondary" disabled={busy} onClick={() => submit("followup")}>
              Programar follow-up
            </Button>
          </div>

          <div className="space-y-2">
            <Label>Pendentes</Label>
            {items.length === 0 ? (
              <p className="text-xs text-muted-foreground">Nenhum agendamento pendente.</p>
            ) : (
              <ul className="max-h-48 space-y-1 overflow-y-auto">
                {items.map((it) => (
                  <li key={it.id} className="flex items-center gap-2 rounded-md border px-2 py-1.5 text-xs">
                    <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] uppercase">
                      {it.kind === "followup" ? "follow-up" : "agendada"}
                    </span>
                    <span className="tabular-nums text-muted-foreground">
                      {new Date(it.runAt).toLocaleString()}
                    </span>
                    <span className="flex-1 truncate">{it.text}</span>
                    <button type="button" onClick={() => void remove(it.id)} className="text-muted-foreground hover:text-destructive">
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
};
