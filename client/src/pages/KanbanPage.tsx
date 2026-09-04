import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import { Plus, Trash2, MessageSquare, Loader2, Timer, AlertTriangle, Save } from "lucide-react";
import {
  DndContext,
  PointerSensor,
  useSensor,
  useSensors,
  closestCorners,
  DragOverlay,
  useDroppable,
  type DragEndEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useNavigate } from "react-router-dom";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import {
  listBoards,
  getBoard,
  createBoard,
  deleteBoard,
  createColumn,
  deleteColumn,
  updateColumn,
  createCard,
  deleteCard,
  moveCard,
  updateCard,
} from "@/services/kanban";
import type {
  KanbanBoard,
  KanbanColumn,
  KanbanCard,
  BoardSnapshot,
} from "@/types/kanban";
import { useChats } from "@/stores/chats";
import { cn } from "@/lib/utils";

const DEFAULT_COLORS = ["#4E93FF", "#22c55e", "#f59e0b", "#ef4444", "#a855f7", "#0ea5e9"];

// 3 ready-made templates the user can pick when creating a new board.
type BoardTemplateKey = "atendimento" | "vendas" | "suporte";
type BoardTemplate = {
  label: string;
  color: string;
  cols: { name: string; color: string; type: "open" | "won" | "lost" }[];
};

// ---- SLA helpers -----------------------------------------------------
// O SLA é configurado por etapa (coluna), em horas. Um cartão que ficar
// parado na etapa por mais tempo que o SLA é sinalizado em vermelho.
const HOUR_MS = 3600_000;

const cardAgeMs = (card: KanbanCard) => {
  const ts = (card.updatedAt || card.createdAt || 0) * 1000;
  return ts > 0 ? Date.now() - ts : 0;
};

const slaState = (card: KanbanCard, slaHours?: number) => {
  const hours = slaHours ?? 0;
  if (!hours) return { enabled: false, breached: false, warn: false, ageMs: cardAgeMs(card) };
  const ageMs = cardAgeMs(card);
  const limit = hours * HOUR_MS;
  return { enabled: true, breached: ageMs > limit, warn: ageMs > limit * 0.75, ageMs };
};

const fmtAge = (ms: number) => {
  const h = Math.floor(ms / HOUR_MS);
  if (h < 1) return `${Math.max(1, Math.floor(ms / 60000))}min`;
  if (h < 48) return `${h}h`;
  return `${Math.floor(h / 24)}d`;
};

function SortableCard({
  card,
  avatarUrl,
  contactName,
  slaHours,
  onOpen,
  onDelete,
}: {
  card: KanbanCard;
  avatarUrl?: string;
  contactName?: string;
  slaHours?: number;
  onOpen: (c: KanbanCard) => void;
  onDelete: (c: KanbanCard) => void;
}) {
  const { t } = useTranslation();
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: card.id,
    data: { type: "card", card },
  });
  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.4 : 1,
  };
  const displayName = contactName || card.title;
  const initials = (displayName || "??").slice(0, 2).toUpperCase();
  const sla = slaState(card, slaHours);
  return (
    <div
      ref={setNodeRef}
      style={style}
      {...attributes}
      {...listeners}
      className="group rounded-md border bg-card p-3 shadow-sm hover:border-primary/40 cursor-grab active:cursor-grabbing"
    >
      <div className="flex items-start gap-2">
        <div className="h-8 w-8 shrink-0 overflow-hidden rounded-full bg-muted grid place-items-center text-[10px] font-medium text-muted-foreground">
          {avatarUrl ? (
            <img src={avatarUrl} alt={displayName} className="h-full w-full object-cover" />
          ) : (
            <span>{initials}</span>
          )}
        </div>
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium leading-snug">{displayName}</p>
          {card.title !== displayName && (
            <p className="truncate text-[11px] text-muted-foreground">{card.title}</p>
          )}
        </div>
        <button
          onClick={(e) => {
            e.stopPropagation();
            onDelete(card);
          }}
          className="opacity-0 transition group-hover:opacity-100 text-muted-foreground hover:text-destructive"
          aria-label={t("pages.kanban.remove", { defaultValue: "Remover" })}
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </div>
      {card.description && (
        <p className="mt-1 text-xs text-muted-foreground line-clamp-2">{card.description}</p>
      )}
      {sla.enabled && (
        <span
          className={cn(
            "mt-2 inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-medium",
            sla.breached
              ? "bg-destructive/15 text-destructive"
              : sla.warn
                ? "bg-amber-500/15 text-amber-600 dark:text-amber-400"
                : "bg-muted text-muted-foreground",
          )}
          title={t("pages.kanban.slaStageTitle", { defaultValue: "SLA da etapa: {{hours}}h", hours: slaHours })}
        >
          {sla.breached ? <AlertTriangle className="h-3 w-3" /> : <Timer className="h-3 w-3" />}
          {sla.breached
            ? t("pages.kanban.slaBreached", { defaultValue: "Fora do SLA · {{age}}", age: fmtAge(sla.ageMs) })
            : t("pages.kanban.slaOk", { defaultValue: "{{age}} / {{hours}}h", age: fmtAge(sla.ageMs), hours: slaHours })}
        </span>
      )}
      {card.chatJid && (
        <button
          onClick={(e) => {
            e.stopPropagation();
            onOpen(card);
          }}
          className="mt-2 inline-flex items-center gap-1 text-[11px] text-primary hover:underline"
        >
          <MessageSquare className="h-3 w-3" />
          {t("pages.kanban.openChat", { defaultValue: "Abrir chat" })}
        </button>
      )}
    </div>
  );
}

function ColumnView({
  column,
  cards,
  cardMeta,
  onAddCard,
  onDeleteColumn,
  onEditSla,
  onOpenCard,
  onDeleteCard,
}: {
  column: KanbanColumn;
  cards: KanbanCard[];
  cardMeta: (c: KanbanCard) => { avatarUrl?: string; contactName?: string };
  onAddCard: (colId: string) => void;
  onDeleteColumn: (col: KanbanColumn) => void;
  onEditSla: (col: KanbanColumn) => void;
  onOpenCard: (c: KanbanCard) => void;
  onDeleteCard: (c: KanbanCard) => void;
}) {
  const { t } = useTranslation();
  const { setNodeRef, isOver } = useDroppable({ id: column.id, data: { type: "column" } });
  const breached = cards.filter((c) => slaState(c, column.slaHours).breached).length;
  return (
    <div
      ref={setNodeRef}
      className={cn(
        "flex w-72 shrink-0 flex-col rounded-lg bg-muted/40 p-3 transition",
        isOver && "ring-2 ring-primary/40",
      )}
    >
      <div className="mb-2 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className="h-2 w-2 rounded-full" style={{ background: column.color || "#4E93FF" }} />
          <p className="text-sm font-semibold">{column.name}</p>
          <span className="text-xs text-muted-foreground">{cards.length}</span>
          {breached > 0 && (
            <span className="inline-flex items-center gap-1 rounded-full bg-destructive/15 px-1.5 py-0.5 text-[10px] font-medium text-destructive">
              <AlertTriangle className="h-3 w-3" />
              {breached}
            </span>
          )}
        </div>
        <div className="flex items-center gap-1.5">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => onEditSla(column)}
            className={cn(
              "h-7 gap-1 px-1.5 text-[10px] hover:text-foreground",
              column.slaHours ? "text-muted-foreground" : "text-muted-foreground/60",
            )}
            aria-label={t("pages.kanban.configureSla", { defaultValue: "Configurar SLA da etapa" })}
            title={t("pages.kanban.configureSla", { defaultValue: "Configurar SLA da etapa" })}
          >
            <Timer className="h-3.5 w-3.5" />
            {column.slaHours ? `${column.slaHours}h` : t("pages.kanban.slaLabel", { defaultValue: "SLA" })}
          </Button>
          <button
            onClick={() => onDeleteColumn(column)}
            className="text-muted-foreground hover:text-destructive"
            aria-label={t("pages.kanban.removeColumn", { defaultValue: "Remover coluna" })}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>
      <SortableContext items={cards.map((c) => c.id)} strategy={verticalListSortingStrategy}>
        <div className="flex flex-1 flex-col gap-2 min-h-[60px]">
          {cards.map((c) => {
            const meta = cardMeta(c);
            return (
              <SortableCard
                key={c.id}
                card={c}
                avatarUrl={meta.avatarUrl}
                contactName={meta.contactName}
                slaHours={column.slaHours}
                onOpen={onOpenCard}
                onDelete={onDeleteCard}
              />
            );
          })}
        </div>
      </SortableContext>
      <Button
        variant="ghost"
        size="sm"
        className="mt-2 justify-start text-muted-foreground"
        onClick={() => onAddCard(column.id)}
      >
        <Plus className="h-3.5 w-3.5" />
        {t("pages.kanban.newCard", { defaultValue: "Novo cartão" })}
      </Button>
    </div>
  );
}

// Droppable board tab used to move a card across boards by dragging it onto
// another board's tab. Uses the `board:` prefix on the droppable id so the
// drag-end handler can distinguish it from column/card drops.
function BoardTab({
  board,
  active,
  onClick,
}: {
  board: KanbanBoard;
  active: boolean;
  onClick: () => void;
}) {
  const { setNodeRef, isOver } = useDroppable({ id: `board:${board.id}`, data: { type: "board" } });
  return (
    <button
      ref={setNodeRef}
      onClick={onClick}
      className={cn(
        "flex items-center gap-2 whitespace-nowrap rounded-md border px-3 py-1.5 text-sm transition",
        active
          ? "border-primary/60 bg-primary/10 text-foreground"
          : "border-border/60 bg-card text-muted-foreground hover:text-foreground",
        isOver && "ring-2 ring-primary/60",
      )}
    >
      <span className="h-2 w-2 rounded-full" style={{ background: board.color || "#4E93FF" }} />
      {board.name}
    </button>
  );
}

export default function KanbanPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [boards, setBoards] = useState<KanbanBoard[]>([]);
  const [activeBoardId, setActiveBoardId] = useState<string>("");
  const [snapshot, setSnapshot] = useState<BoardSnapshot | null>(null);
  const [loading, setLoading] = useState(false);
  const [creatingBoard, setCreatingBoard] = useState(false);
  const [newBoardName, setNewBoardName] = useState("");
  const [newBoardTemplate, setNewBoardTemplate] = useState<BoardTemplateKey>("atendimento");
  const [creatingColumn, setCreatingColumn] = useState(false);
  const [newColumnName, setNewColumnName] = useState("");
  const [creatingCard, setCreatingCard] = useState<string | null>(null);
  const [newCardTitle, setNewCardTitle] = useState("");
  const [newCardDesc, setNewCardDesc] = useState("");
  const [toDeleteBoard, setToDeleteBoard] = useState<KanbanBoard | null>(null);
  const [toDeleteColumn, setToDeleteColumn] = useState<KanbanColumn | null>(null);
  const [toDeleteCard, setToDeleteCard] = useState<KanbanCard | null>(null);
  const [slaColumn, setSlaColumn] = useState<KanbanColumn | null>(null);
  const [slaValue, setSlaValue] = useState<string>("0");
  const [dragCard, setDragCard] = useState<KanbanCard | null>(null);
  const [seeded, setSeeded] = useState(false);

  const BOARD_TEMPLATES: Record<BoardTemplateKey, BoardTemplate> = useMemo(() => ({
    atendimento: {
      label: t("pages.kanban.templateSupportLabel", { defaultValue: "Atendimento" }),
      color: "#4E93FF",
      cols: [
        { name: t("pages.kanban.colWaiting", { defaultValue: "Aguardando" }), color: "#94a3b8", type: "open" },
        { name: t("pages.kanban.colInProgress", { defaultValue: "Em andamento" }), color: "#4E93FF", type: "open" },
        { name: t("pages.kanban.colWaitingClient", { defaultValue: "Aguardando cliente" }), color: "#f59e0b", type: "open" },
        { name: t("pages.kanban.colResolved", { defaultValue: "Resolvido" }), color: "#22c55e", type: "won" },
        { name: t("pages.kanban.colNoResponse", { defaultValue: "Sem retorno" }), color: "#ef4444", type: "lost" },
      ],
    },
    vendas: {
      label: t("pages.kanban.templateSalesLabel", { defaultValue: "Vendas" }),
      color: "#22c55e",
      cols: [
        { name: t("pages.kanban.colProspecting", { defaultValue: "Prospecção" }), color: "#94a3b8", type: "open" },
        { name: t("pages.kanban.colProposal", { defaultValue: "Proposta" }), color: "#4E93FF", type: "open" },
        { name: t("pages.kanban.colNegotiation", { defaultValue: "Negociação" }), color: "#a855f7", type: "open" },
        { name: t("pages.kanban.colWon", { defaultValue: "Ganho" }), color: "#22c55e", type: "won" },
        { name: t("pages.kanban.colLost", { defaultValue: "Perdido" }), color: "#ef4444", type: "lost" },
      ],
    },
    suporte: {
      label: t("pages.kanban.templateHelpdeskLabel", { defaultValue: "Suporte" }),
      color: "#f59e0b",
      cols: [
        { name: t("pages.kanban.colNewTicket", { defaultValue: "Novo ticket" }), color: "#94a3b8", type: "open" },
        { name: t("pages.kanban.colInReview", { defaultValue: "Em análise" }), color: "#4E93FF", type: "open" },
        { name: t("pages.kanban.colWaitingClient", { defaultValue: "Aguardando cliente" }), color: "#f59e0b", type: "open" },
        { name: t("pages.kanban.colResolved", { defaultValue: "Resolvido" }), color: "#22c55e", type: "won" },
      ],
    },
  }), [t]);

  const chatsBySession = useChats((s) => s.chatsBySession);
  // Fast lookup for card avatar / contact name based on the linked chat.
  const chatMetaByKey = useMemo(() => {
    const map = new Map<string, { avatarUrl?: string; name?: string }>();
    for (const [sid, list] of Object.entries(chatsBySession)) {
      for (const c of list) {
        map.set(`${sid}::${c.chatJid}`, { avatarUrl: c.avatarUrl, name: c.name });
      }
    }
    return map;
  }, [chatsBySession]);
  const cardMeta = (card: KanbanCard) => {
    if (!card.chatJid) return {};
    const key = `${card.sessionId ?? ""}::${card.chatJid}`;
    const m = chatMetaByKey.get(key);
    return { avatarUrl: m?.avatarUrl, contactName: m?.name };
  };

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }));

  const loadBoards = async () => {
    try {
      const bs = await listBoards();
      setBoards(bs);
      if (bs.length && !activeBoardId) setActiveBoardId(bs[0].id);
      if (!bs.length) {
        setActiveBoardId("");
        setSnapshot(null);
      }
      return bs;
    } catch (e) {
      toast.error((e as Error).message);
      return [] as KanbanBoard[];
    }
  };

  const loadSnapshot = async (id: string) => {
    if (!id) return;
    setLoading(true);
    try {
      const snap = await getBoard(id);
      setSnapshot(snap);
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    (async () => {
      const bs = await loadBoards();
      if (!seeded && bs.length === 0) {
        setSeeded(true);
        await seedExampleBoards();
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useEffect(() => {
    if (activeBoardId) void loadSnapshot(activeBoardId);
  }, [activeBoardId]);

  const cardsByColumn = useMemo(() => {
    const map = new Map<string, KanbanCard[]>();
    if (!snapshot) return map;
    for (const col of snapshot.columns) map.set(col.id, []);
    for (const card of [...snapshot.cards].sort((a, b) => a.position - b.position)) {
      const arr = map.get(card.columnId);
      if (arr) arr.push(card);
    }
    return map;
  }, [snapshot]);

  const columns = useMemo(
    () => (snapshot?.columns ?? []).slice().sort((a, b) => a.position - b.position),
    [snapshot],
  );

  const handleCreateBoard = async () => {
    const name = newBoardName.trim();
    if (!name) return;
    const tpl = BOARD_TEMPLATES[newBoardTemplate];
    try {
      const b = await createBoard(name, tpl.color);
      for (const c of tpl.cols) await createColumn(b.id, c.name, c.color, c.type);
      setNewBoardName("");
      setCreatingBoard(false);
      await loadBoards();
      setActiveBoardId(b.id);
      toast.success(t("pages.kanban.boardCreatedToast", { defaultValue: "Board criado" }));
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  const seedExampleBoards = async () => {
    try {
      const examples: { name: string; color: string; cols: { name: string; color: string; type: "open" | "won" | "lost" }[] }[] = [
        {
          name: t("pages.kanban.templateSupportLabel", { defaultValue: "Atendimento" }),
          color: "#4E93FF",
          cols: [
            { name: t("pages.kanban.colInProgress", { defaultValue: "Em andamento" }), color: "#4E93FF", type: "open" },
            { name: t("pages.kanban.colWaitingClient", { defaultValue: "Aguardando cliente" }), color: "#f59e0b", type: "open" },
            { name: t("pages.kanban.colResolved", { defaultValue: "Resolvido" }), color: "#22c55e", type: "won" },
            { name: t("pages.kanban.colNoResponse", { defaultValue: "Sem retorno" }), color: "#ef4444", type: "lost" },
          ],
        },
        {
          name: t("pages.kanban.templateSalesLabel", { defaultValue: "Vendas" }),
          color: "#22c55e",
          cols: [
            { name: t("pages.kanban.colProspecting", { defaultValue: "Prospecção" }), color: "#94a3b8", type: "open" },
            { name: t("pages.kanban.colProposalSent", { defaultValue: "Proposta enviada" }), color: "#4E93FF", type: "open" },
            { name: t("pages.kanban.colNegotiation", { defaultValue: "Negociação" }), color: "#a855f7", type: "open" },
            { name: t("pages.kanban.colClosedWon", { defaultValue: "Fechado - Ganho" }), color: "#22c55e", type: "won" },
            { name: t("pages.kanban.colClosedLost", { defaultValue: "Fechado - Perdido" }), color: "#ef4444", type: "lost" },
          ],
        },
        {
          name: t("pages.kanban.templateHelpdeskLabel", { defaultValue: "Suporte" }),
          color: "#f59e0b",
          cols: [
            { name: t("pages.kanban.colNewTicket", { defaultValue: "Novo ticket" }), color: "#94a3b8", type: "open" },
            { name: t("pages.kanban.colInReview", { defaultValue: "Em análise" }), color: "#4E93FF", type: "open" },
            { name: t("pages.kanban.colWaitingClient", { defaultValue: "Aguardando cliente" }), color: "#f59e0b", type: "open" },
            { name: t("pages.kanban.colResolved", { defaultValue: "Resolvido" }), color: "#22c55e", type: "won" },
          ],
        },
      ];
      for (const ex of examples) {
        const b = await createBoard(ex.name, ex.color, t("pages.kanban.exampleBoardDesc", { defaultValue: "Board exemplo — {{name}}", name: ex.name }));
        for (const c of ex.cols) await createColumn(b.id, c.name, c.color, c.type);
      }
      const bs = await loadBoards();
      if (bs.length) setActiveBoardId(bs[0].id);
      toast.success(t("pages.kanban.exampleBoardsCreatedToast", { defaultValue: "3 boards de exemplo criados" }));
    } catch (e) {
      toast.error(t("pages.kanban.errSeed", { defaultValue: "Não foi possível criar boards de exemplo: {{msg}}", msg: (e as Error).message }));
    }
  };

  const handleCreateColumn = async () => {
    const name = newColumnName.trim();
    if (!name || !activeBoardId) return;
    try {
      await createColumn(activeBoardId, name, DEFAULT_COLORS[(columns.length ?? 0) % DEFAULT_COLORS.length]);
      setNewColumnName("");
      setCreatingColumn(false);
      await loadSnapshot(activeBoardId);
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  const openSlaDialog = (col: KanbanColumn) => {
    setSlaColumn(col);
    setSlaValue(String(col.slaHours ?? 0));
  };

  const handleSaveSla = async () => {
    if (!slaColumn) return;
    const hours = Math.max(0, Number(slaValue) || 0);
    try {
      await updateColumn(slaColumn.id, slaColumn.name, slaColumn.color, slaColumn.stageType, hours);
      setSlaColumn(null);
      await loadSnapshot(activeBoardId);
      toast.success(
        hours
          ? t("pages.kanban.slaSetToast", { defaultValue: "SLA da etapa definido em {{hours}}h", hours })
          : t("pages.kanban.slaDisabledToast", { defaultValue: "SLA da etapa desativado" }),
      );
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  const handleCreateCard = async () => {
    if (!creatingCard) return;
    const title = newCardTitle.trim();
    if (!title) return;
    try {
      await createCard(activeBoardId, {
        columnId: creatingCard,
        title,
        description: newCardDesc.trim() || undefined,
      });
      setNewCardTitle("");
      setNewCardDesc("");
      setCreatingCard(null);
      await loadSnapshot(activeBoardId);
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  const handleDragStart = (e: DragStartEvent) => {
    const card = snapshot?.cards.find((c) => c.id === e.active.id);
    setDragCard(card ?? null);
  };

  const handleDragEnd = async (e: DragEndEvent) => {
    setDragCard(null);
    if (!snapshot) return;
    const { active, over } = e;
    if (!over) return;
    const activeCard = snapshot.cards.find((c) => c.id === active.id);
    if (!activeCard) return;

    // Dropped on another board's tab → cross-board move: create card on the
    // target board's first column and remove the original.
    if (typeof over.id === "string" && over.id.startsWith("board:")) {
      const targetBoardId = over.id.slice("board:".length);
      if (targetBoardId === activeBoardId) return;
      try {
        const targetSnap = await getBoard(targetBoardId);
        const firstCol = [...targetSnap.columns].sort((a, b) => a.position - b.position)[0];
        if (!firstCol) {
          toast.error(t("pages.kanban.errNoTargetColumns", { defaultValue: "Board de destino não tem colunas" }));
          return;
        }
        await createCard(targetBoardId, {
          columnId: firstCol.id,
          title: activeCard.title,
          description: activeCard.description || undefined,
          color: activeCard.color || undefined,
          sessionId: activeCard.sessionId,
          chatJid: activeCard.chatJid,
          assigneeId: activeCard.assigneeId,
          dueAt: activeCard.dueAt,
        });
        await deleteCard(activeCard.id);
        await loadSnapshot(activeBoardId);
        toast.success(t("pages.kanban.cardMovedToast", { defaultValue: "Cartão movido de board" }));
      } catch (err) {
        toast.error((err as Error).message);
      }
      return;
    }

    // Determine target column: either dropped on a card (same/other column) or column id
    let targetColumnId = activeCard.columnId;
    let targetIndex = 0;
    const overCard = snapshot.cards.find((c) => c.id === over.id);
    if (overCard) {
      targetColumnId = overCard.columnId;
      const list = (cardsByColumn.get(targetColumnId) ?? []).filter((c) => c.id !== activeCard.id);
      const idx = list.findIndex((c) => c.id === overCard.id);
      targetIndex = idx < 0 ? list.length : idx;
    } else if (typeof over.id === "string" && snapshot.columns.some((c) => c.id === over.id)) {
      targetColumnId = over.id;
      targetIndex = (cardsByColumn.get(targetColumnId)?.length ?? 0);
    }

    // Optimistic update
    const prev = snapshot;
    const nextCards = snapshot.cards.map((c) => ({ ...c }));
    const moving = nextCards.find((c) => c.id === activeCard.id);
    if (!moving) return;
    moving.columnId = targetColumnId;
    // reindex within target column
    const inCol = nextCards
      .filter((c) => c.columnId === targetColumnId && c.id !== moving.id)
      .sort((a, b) => a.position - b.position);
    inCol.splice(targetIndex, 0, moving);
    inCol.forEach((c, i) => (c.position = i));
    setSnapshot({ ...snapshot, cards: nextCards });

    try {
      await moveCard(activeCard.id, targetColumnId, targetIndex);
    } catch (err) {
      toast.error((err as Error).message);
      setSnapshot(prev);
    }
  };

  const openChat = (c: KanbanCard) => {
    if (!c.chatJid) return;
    navigate(`/chats?jid=${encodeURIComponent(c.chatJid)}${c.sessionId ? `&sid=${c.sessionId}` : ""}`);
  };

  return (
    <AppShell>
      <div className="flex h-full flex-col">
      <div className="flex items-center justify-between gap-2 border-b px-4 py-3">
        <div className="flex min-w-0 flex-1 items-center gap-2 overflow-x-auto scrollbar-soft">
          {boards.map((b) => (
            <BoardTab
              key={b.id}
              board={b}
              active={b.id === activeBoardId}
              onClick={() => setActiveBoardId(b.id)}
            />
          ))}
          <Button variant="outline" size="sm" className="shrink-0" onClick={() => setCreatingBoard(true)}>
            <Plus className="h-4 w-4" /> {t("pages.kanban.newBoard", { defaultValue: "Novo board" })}
          </Button>
          {activeBoardId && (
            <Button
              variant="ghost"
              size="sm"
              className="shrink-0 text-destructive"
              onClick={() => {
                const b = boards.find((x) => x.id === activeBoardId);
                if (b) setToDeleteBoard(b);
              }}
            >
              <Trash2 className="h-4 w-4" /> {t("pages.kanban.removeBoard", { defaultValue: "Remover board" })}
            </Button>
          )}
        </div>
        {activeBoardId && (
          <Button size="sm" className="shrink-0" onClick={() => setCreatingColumn(true)}>
            <Plus className="h-4 w-4" /> {t("pages.kanban.newColumnButton", { defaultValue: "Nova coluna" })}
          </Button>
        )}
      </div>

      <div className="flex-1 overflow-auto scrollbar-soft p-4">
        {loading && (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" /> {t("common.loading", { defaultValue: "Carregando..." })}
          </div>
        )}
        {!loading && !activeBoardId && (
          <div className="flex h-full flex-col items-center justify-center gap-3 text-center">
            <p className="text-muted-foreground">{t("pages.kanban.noBoardsYet", { defaultValue: "Nenhum board ainda." })}</p>
            <Button onClick={() => setCreatingBoard(true)}>
              <Plus className="h-4 w-4" /> {t("pages.kanban.createFirstBoard", { defaultValue: "Criar primeiro board" })}
            </Button>
          </div>
        )}
        {!loading && activeBoardId && snapshot && (
          <DndContext
            sensors={sensors}
            collisionDetection={closestCorners}
            onDragStart={handleDragStart}
            onDragEnd={handleDragEnd}
          >
            <div className="flex gap-4 pb-4">
              {columns.map((col) => (
                <ColumnView
                  key={col.id}
                  column={col}
                  cards={cardsByColumn.get(col.id) ?? []}
                  cardMeta={cardMeta}
                  onAddCard={(id) => {
                    setCreatingCard(id);
                    setNewCardTitle("");
                    setNewCardDesc("");
                  }}
                  onDeleteColumn={setToDeleteColumn}
                  onEditSla={openSlaDialog}
                  onOpenCard={openChat}
                  onDeleteCard={setToDeleteCard}
                />
              ))}
              {!columns.length && (
                <div className="text-sm text-muted-foreground">
                  {t("pages.kanban.noColumns", { defaultValue: 'Nenhuma coluna. Clique em "Nova coluna" para começar.' })}
                </div>
              )}
            </div>
            <DragOverlay>
              {dragCard && (
                <div className="rounded-md border bg-card p-3 shadow-lg text-sm font-medium">
                  {dragCard.title}
                </div>
              )}
            </DragOverlay>
          </DndContext>
        )}
      </div>

      {/* SLA da etapa */}
      <Dialog open={!!slaColumn} onOpenChange={(o) => !o && setSlaColumn(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Timer className="h-4 w-4 text-primary" />
              {t("pages.kanban.slaDialogTitle", { defaultValue: "SLA da etapa {{name}}", name: slaColumn?.name })}
            </DialogTitle>
          </DialogHeader>
          <div className="space-y-3 py-1">
            <label htmlFor="kanban-sla-hours" className="text-xs font-medium">{t("pages.kanban.hourLimitLabel", { defaultValue: "Limite em horas" })}</label>
            <Input
              id="kanban-sla-hours"
              type="number"
              min={0}
              step={1}
              value={slaValue}
              onChange={(e) => setSlaValue(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") void handleSaveSla();
              }}
              autoFocus
            />
            <p className="text-xs text-muted-foreground">
              {t("pages.kanban.slaHint", { defaultValue: "Use 0 para desativar. Cartões parados além do limite recebem alerta nesta etapa." })}
            </p>
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setSlaColumn(null)}>{t("common.cancel", { defaultValue: "Cancelar" })}</Button>
            <Button onClick={handleSaveSla}>
              <Save className="h-4 w-4" /> {t("pages.kanban.saveSla", { defaultValue: "Salvar SLA" })}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* New board */}
      <Dialog open={creatingBoard} onOpenChange={setCreatingBoard}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("pages.kanban.newBoard", { defaultValue: "Novo board" })}</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <div>
              <label className="text-xs font-medium">{t("common.name", { defaultValue: "Nome" })}</label>
              <Input
                placeholder={t("pages.kanban.boardNamePlaceholder", { defaultValue: "Nome do board" })}
                value={newBoardName}
                onChange={(e) => setNewBoardName(e.target.value)}
                autoFocus
              />
            </div>
            <div>
              <label className="text-xs font-medium">{t("pages.kanban.modelLabel", { defaultValue: "Modelo" })}</label>
              <div className="mt-1 grid grid-cols-3 gap-2">
                {(Object.keys(BOARD_TEMPLATES) as BoardTemplateKey[]).map((k) => {
                  const tpl = BOARD_TEMPLATES[k];
                  const active = newBoardTemplate === k;
                  return (
                    <button
                      key={k}
                      type="button"
                      onClick={() => setNewBoardTemplate(k)}
                      className={cn(
                        "rounded-md border p-2 text-left text-xs transition",
                        active ? "border-primary bg-primary/10" : "border-border hover:border-primary/40",
                      )}
                    >
                      <div className="flex items-center gap-1.5">
                        <span className="h-2 w-2 rounded-full" style={{ background: tpl.color }} />
                        <span className="font-medium">{tpl.label}</span>
                      </div>
                      <p className="mt-1 text-[10px] text-muted-foreground line-clamp-2">
                        {tpl.cols.map((c) => c.name).join(" · ")}
                      </p>
                    </button>
                  );
                })}
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setCreatingBoard(false)}>
              {t("common.cancel", { defaultValue: "Cancelar" })}
            </Button>
            <Button onClick={handleCreateBoard}>{t("pages.kanban.create", { defaultValue: "Criar" })}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* New column */}
      <Dialog open={creatingColumn} onOpenChange={setCreatingColumn}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("pages.kanban.newColumnButton", { defaultValue: "Nova coluna" })}</DialogTitle>
          </DialogHeader>
          <Input
            placeholder={t("pages.kanban.columnNamePlaceholder", { defaultValue: "Nome da coluna" })}
            value={newColumnName}
            onChange={(e) => setNewColumnName(e.target.value)}
            autoFocus
          />
          <DialogFooter>
            <Button variant="ghost" onClick={() => setCreatingColumn(false)}>
              {t("common.cancel", { defaultValue: "Cancelar" })}
            </Button>
            <Button onClick={handleCreateColumn}>{t("pages.kanban.create", { defaultValue: "Criar" })}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* New card */}
      <Dialog open={!!creatingCard} onOpenChange={(o) => !o && setCreatingCard(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("pages.kanban.newCardDialogTitle", { defaultValue: "Novo cartão" })}</DialogTitle>
          </DialogHeader>
          <Input
            placeholder={t("pages.kanban.cardTitlePlaceholder", { defaultValue: "Título" })}
            value={newCardTitle}
            onChange={(e) => setNewCardTitle(e.target.value)}
            autoFocus
          />
          <Textarea
            placeholder={t("pages.kanban.cardDescPlaceholder", { defaultValue: "Descrição (opcional)" })}
            value={newCardDesc}
            onChange={(e) => setNewCardDesc(e.target.value)}
            rows={3}
          />
          <DialogFooter>
            <Button variant="ghost" onClick={() => setCreatingCard(null)}>
              {t("common.cancel", { defaultValue: "Cancelar" })}
            </Button>
            <Button onClick={handleCreateCard}>{t("pages.kanban.create", { defaultValue: "Criar" })}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={!!toDeleteBoard}
        onOpenChange={(o) => !o && setToDeleteBoard(null)}
        title={t("pages.kanban.deleteBoardTitle", { defaultValue: "Remover board?" })}
        description={toDeleteBoard ? t("pages.kanban.deleteBoardDescription", { defaultValue: "{{name}} e todos os cartões serão apagados.", name: toDeleteBoard.name }) : undefined}
        confirmLabel={t("pages.kanban.remove", { defaultValue: "Remover" })}
        destructive
        onConfirm={async () => {
          if (!toDeleteBoard) return;
          try {
            await deleteBoard(toDeleteBoard.id);
            setActiveBoardId("");
            setSnapshot(null);
            await loadBoards();
          } catch (e) {
            toast.error((e as Error).message);
          }
        }}
      />
      <ConfirmDialog
        open={!!toDeleteColumn}
        onOpenChange={(o) => !o && setToDeleteColumn(null)}
        title={t("pages.kanban.deleteColumnTitle", { defaultValue: "Remover coluna?" })}
        description={toDeleteColumn ? t("pages.kanban.deleteColumnDescription", { defaultValue: "A coluna {{name}} e seus cartões serão apagados.", name: toDeleteColumn.name }) : undefined}
        confirmLabel={t("pages.kanban.remove", { defaultValue: "Remover" })}
        destructive
        onConfirm={async () => {
          if (!toDeleteColumn) return;
          try {
            await deleteColumn(toDeleteColumn.id);
            await loadSnapshot(activeBoardId);
          } catch (e) {
            toast.error((e as Error).message);
          }
        }}
      />
      <ConfirmDialog
        open={!!toDeleteCard}
        onOpenChange={(o) => !o && setToDeleteCard(null)}
        title={t("pages.kanban.deleteCardTitle", { defaultValue: "Remover cartão?" })}
        confirmLabel={t("pages.kanban.remove", { defaultValue: "Remover" })}
        destructive
        onConfirm={async () => {
          if (!toDeleteCard) return;
          try {
            await deleteCard(toDeleteCard.id);
            await loadSnapshot(activeBoardId);
          } catch (e) {
            toast.error((e as Error).message);
          }
        }}
      />

      </div>
    </AppShell>
  );
}
