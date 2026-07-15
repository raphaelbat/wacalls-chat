import { useEffect, useState } from "react";
import { Loader2, Plus, Trash2, Pencil, Users2 } from "lucide-react";
import { toast } from "sonner";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  listQueues,
  createQueue,
  updateQueue,
  deleteQueue,
} from "@/services/queues";
import type { Queue } from "@/types/queue";

const DEFAULT_COLORS = [
  "#57adf8", "#10b981", "#f59e0b", "#f43f5e", "#8b5cf6", "#06b6d4", "#eab308",
];

type Editing = { queue: Queue | null; name: string; color: string; greeting: string };

const emptyEditing = (): Editing => ({
  queue: null,
  name: "",
  color: DEFAULT_COLORS[0],
  greeting: "",
});

export default function QueuesPage() {
  const [queues, setQueues] = useState<Queue[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [modal, setModal] = useState<Editing | null>(null);
  const [toDelete, setToDelete] = useState<Queue | null>(null);

  const load = async () => {
    setLoading(true);
    try {
      const list = await listQueues();
      setQueues(list);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Erro ao carregar filas");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const onSave = async () => {
    if (!modal || !modal.name.trim()) return;
    setSaving(true);
    try {
      if (modal.queue) {
        await updateQueue(modal.queue.id, modal.name.trim(), modal.color, {
          greeting: modal.greeting,
        });
        toast.success("Fila atualizada");
      } else {
        const q = await createQueue(modal.name.trim(), modal.color);
        if (modal.greeting.trim()) {
          await updateQueue(q.id, modal.name.trim(), modal.color, { greeting: modal.greeting });
        }
        toast.success("Fila criada");
      }
      setModal(null);
      load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Erro ao salvar");
    } finally {
      setSaving(false);
    }
  };

  const onDelete = async (q: Queue) => {
    try {
      await deleteQueue(q.id);
      toast.success("Fila removida");
      load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Erro ao remover");
    }
  };

  return (
    <AppShell>
      <div className="space-y-5 pb-12">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold tracking-tight flex items-center gap-2">
              <Users2 className="h-5 w-5 text-primary" /> Filas
            </h2>
            <p className="text-sm text-muted-foreground">
              Organize atendimentos em filas e vincule a conexões e usuários.
            </p>
          </div>
          <Button onClick={() => setModal(emptyEditing())}>
            <Plus className="h-4 w-4" /> Nova fila
          </Button>
        </div>

        {loading ? (
          <div className="flex justify-center py-12">
            <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
          </div>
        ) : queues.length === 0 ? (
          <div className="grid place-items-center rounded-xl border border-dashed bg-card/40 p-12 text-center">
            <div className="grid h-12 w-12 place-items-center rounded-full bg-muted text-muted-foreground">
              <Users2 className="h-5 w-5" />
            </div>
            <div className="mt-3 text-sm font-medium">Nenhuma fila criada</div>
            <div className="mt-1 text-xs text-muted-foreground">
              Crie sua primeira fila para começar a organizar atendimentos.
            </div>
            <Button className="mt-4" onClick={() => setModal(emptyEditing())}>
              <Plus className="h-4 w-4" /> Nova fila
            </Button>
          </div>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {queues.map((q) => (
              <div key={q.id} className="rounded-xl border bg-card p-4">
                <div className="flex items-start justify-between gap-3">
                  <div className="flex items-center gap-2">
                    <span
                      className="h-8 w-8 shrink-0 rounded-lg"
                      style={{ backgroundColor: q.color }}
                    />
                    <div className="min-w-0">
                      <p className="truncate text-sm font-semibold">{q.name}</p>
                    </div>

                  </div>
                  <div className="flex items-center gap-1">
                    <Button
                      size="icon"
                      variant="ghost"
                      onClick={() =>
                        setModal({
                          queue: q,
                          name: q.name,
                          color: q.color,
                          greeting: q.greeting ?? "",
                        })
                      }
                    >
                      <Pencil className="h-4 w-4" />
                    </Button>
                    <Button
                      size="icon"
                      variant="ghost"
                      className="text-destructive"
                      onClick={() => setToDelete(q)}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              </div>

            ))}
          </div>
        )}
      </div>

      <Dialog open={!!modal} onOpenChange={(o) => !o && setModal(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{modal?.queue ? "Editar fila" : "Nova fila"}</DialogTitle>
          </DialogHeader>
          {modal && (
            <div className="space-y-4">
              <div className="space-y-2">
                <Label>Nome</Label>
                <Input
                  value={modal.name}
                  onChange={(e) => setModal({ ...modal, name: e.target.value })}
                  placeholder="Ex: Vendas, Suporte..."
                />
              </div>
              <div className="space-y-2">
                <Label>Cor</Label>
                <div className="flex flex-wrap gap-2">
                  {DEFAULT_COLORS.map((c) => (
                    <button
                      key={c}
                      type="button"
                      onClick={() => setModal({ ...modal, color: c })}
                      className={`h-8 w-8 rounded-md border-2 transition ${
                        modal.color === c ? "border-foreground" : "border-transparent"
                      }`}
                      style={{ backgroundColor: c }}
                      aria-label={`Cor ${c}`}
                    />
                  ))}
                  <Input
                    type="color"
                    value={modal.color}
                    onChange={(e) => setModal({ ...modal, color: e.target.value })}
                    className="h-8 w-14 cursor-pointer p-1"
                  />
                </div>
              </div>
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setModal(null)}>
              Cancelar
            </Button>
            <Button onClick={onSave} disabled={saving || !modal?.name.trim()}>
              {saving && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Salvar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={!!toDelete}
        onOpenChange={(o) => !o && setToDelete(null)}
        title="Remover fila?"
        description={toDelete ? `A fila "${toDelete.name}" será removida.` : undefined}
        confirmLabel="Remover"
        destructive
        onConfirm={() => {
          if (toDelete) void onDelete(toDelete);
        }}
      />
    </AppShell>
  );
}
