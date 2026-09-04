import { useEffect, useState } from "react";
import { Loader2, Plus, Trash2, Pencil, Users2 } from "lucide-react";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import { BusinessHoursEditor } from "@/components/domain/settings/BusinessHoursEditor";
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

type Editing = {
  queue: Queue | null;
  name: string;
  color: string;
  greeting: string;
  distribution: string;
  maxLoad: number;
};

const emptyEditing = (): Editing => ({
  queue: null,
  name: "",
  color: DEFAULT_COLORS[0],
  greeting: "",
  distribution: "manual",
  maxLoad: 0,
});

export default function QueuesPage() {
  const { t } = useTranslation();
  const [queues, setQueues] = useState<Queue[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [modal, setModal] = useState<Editing | null>(null);
  const [toDelete, setToDelete] = useState<Queue | null>(null);

  const DISTRIBUTIONS = [
    { value: "manual", label: t("pages.queues.distManual", { defaultValue: "Manual (sem distribuição)" }) },
    { value: "round-robin", label: t("pages.queues.distRoundRobin", { defaultValue: "Round-robin (rodízio)" }) },
    { value: "least-busy", label: t("pages.queues.distLeastBusy", { defaultValue: "Menor carga" }) },
    { value: "random", label: t("pages.queues.distRandom", { defaultValue: "Aleatório" }) },
  ];

  const load = async () => {
    setLoading(true);
    try {
      const list = await listQueues();
      setQueues(list);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("pages.queues.errLoad", { defaultValue: "Erro ao carregar filas" }));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const onSave = async () => {
    if (!modal || !modal.name.trim()) return;
    setSaving(true);
    try {
      if (modal.queue) {
        await updateQueue(modal.queue.id, modal.name.trim(), modal.color, {
          greeting: modal.greeting,
          distribution: modal.distribution,
          maxLoad: modal.maxLoad,
        });
        toast.success(t("pages.queues.updatedToast", { defaultValue: "Fila atualizada" }));
      } else {
        const q = await createQueue(modal.name.trim(), modal.color);
        if (modal.greeting.trim()) {
          await updateQueue(q.id, modal.name.trim(), modal.color, { greeting: modal.greeting });
        }
        if (modal.distribution !== "manual" || modal.maxLoad > 0) {
          await updateQueue(q.id, modal.name.trim(), modal.color, {
            greeting: modal.greeting,
            distribution: modal.distribution,
            maxLoad: modal.maxLoad,
          });
        }
        toast.success(t("pages.queues.createdToast", { defaultValue: "Fila criada" }));
      }
      setModal(null);
      load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("pages.queues.errSave", { defaultValue: "Erro ao salvar" }));
    } finally {
      setSaving(false);
    }
  };

  const onDelete = async (q: Queue) => {
    try {
      await deleteQueue(q.id);
      toast.success(t("pages.queues.removedToast", { defaultValue: "Fila removida" }));
      load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("pages.queues.errRemove", { defaultValue: "Erro ao remover" }));
    }
  };

  return (
    <AppShell>
      <div className="space-y-5 pb-12">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold tracking-tight flex items-center gap-2">
              <Users2 className="h-5 w-5 text-primary" /> {t("pages.queues.title", { defaultValue: "Filas" })}
            </h2>
            <p className="text-sm text-muted-foreground">
              {t("pages.queues.subtitle", { defaultValue: "Organize atendimentos em filas e vincule a conexões e usuários." })}
            </p>
          </div>
          <Button onClick={() => setModal(emptyEditing())}>
            <Plus className="h-4 w-4" /> {t("pages.queues.newQueue", { defaultValue: "Nova fila" })}
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
            <div className="mt-3 text-sm font-medium">{t("pages.queues.empty", { defaultValue: "Nenhuma fila criada" })}</div>
            <div className="mt-1 text-xs text-muted-foreground">
              {t("pages.queues.emptyHint", { defaultValue: "Crie sua primeira fila para começar a organizar atendimentos." })}
            </div>
            <Button className="mt-4" onClick={() => setModal(emptyEditing())}>
              <Plus className="h-4 w-4" /> {t("pages.queues.newQueue", { defaultValue: "Nova fila" })}
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
                          distribution: q.distribution || "manual",
                          maxLoad: q.maxLoad ?? 0,
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
        <DialogContent className="max-h-[85vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>
              {modal?.queue
                ? t("pages.queues.editTitle", { defaultValue: "Editar fila" })
                : t("pages.queues.newQueue", { defaultValue: "Nova fila" })}
            </DialogTitle>
          </DialogHeader>
          {modal && (
            <div className="space-y-4">
              <div className="space-y-2">
                <Label>{t("common.name", { defaultValue: "Nome" })}</Label>
                <Input
                  value={modal.name}
                  onChange={(e) => setModal({ ...modal, name: e.target.value })}
                  placeholder={t("pages.queues.namePlaceholder", { defaultValue: "Ex: Vendas, Suporte..." })}
                />
              </div>
              <div className="space-y-2">
                <Label>{t("common.color", { defaultValue: "Cor" })}</Label>
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
                      aria-label={t("pages.queues.colorAria", { defaultValue: "Cor {{color}}", color: c })}
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

              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label>{t("pages.queues.distributionLabel", { defaultValue: "Distribuição automática" })}</Label>
                  <select
                    value={modal.distribution}
                    onChange={(e) => setModal({ ...modal, distribution: e.target.value })}
                    className="flex h-9 w-full rounded-md border border-input bg-background px-2 text-sm shadow-sm focus:outline-none focus:ring-2 focus:ring-ring"
                  >
                    {DISTRIBUTIONS.map((d) => (
                      <option key={d.value} value={d.value}>
                        {d.label}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="space-y-2">
                  <Label>{t("pages.queues.maxLoadLabel", { defaultValue: "Limite de atendimentos por agente" })}</Label>
                  <Input
                    type="number"
                    min={0}
                    value={modal.maxLoad}
                    onChange={(e) =>
                      setModal({ ...modal, maxLoad: Math.max(0, Number(e.target.value) || 0) })
                    }
                    placeholder={t("pages.queues.maxLoadPlaceholder", { defaultValue: "0 = sem limite" })}
                  />
                </div>
                <p className="sm:col-span-2 text-xs text-muted-foreground">
                  {t("pages.queues.autoAssignHint", {
                    defaultValue: "Novas conversas desta fila são atribuídas automaticamente aos agentes vinculados, respeitando o horário de atendimento e o limite de atendimentos simultâneos.",
                  })}
                </p>
              </div>

              {modal.queue && <BusinessHoursEditor scope="queue" scopeId={modal.queue.id} />}
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setModal(null)}>
              {t("common.cancel", { defaultValue: "Cancelar" })}
            </Button>
            <Button onClick={onSave} disabled={saving || !modal?.name.trim()}>
              {saving && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              {t("common.save", { defaultValue: "Salvar" })}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={!!toDelete}
        onOpenChange={(o) => !o && setToDelete(null)}
        title={t("pages.queues.deleteTitle", { defaultValue: "Remover fila?" })}
        description={toDelete ? t("pages.queues.deleteDescription", { defaultValue: 'A fila "{{name}}" será removida.', name: toDelete.name }) : undefined}
        confirmLabel={t("pages.queues.remove", { defaultValue: "Remover" })}
        destructive
        onConfirm={() => {
          if (toDelete) void onDelete(toDelete);
        }}
      />
    </AppShell>
  );
}
