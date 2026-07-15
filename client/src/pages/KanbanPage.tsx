import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { Plus, Trash2, MessageSquare, Loader2 } from "lucide-react";
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
const BOARD_TEMPLATES: Record<BoardTemplateKey, {
  label: string;
  color: string;
  cols: { name: string; color: string; type: "open" | "won" | "lost" }[];
}> = {
  atendimento: {
    label: "Atendimento",
    color: "#4E93FF",
    cols: [
      { name: "Aguardando", color: "#94a3b8", type: "open" },
      { name: "Em andamento", color: "#4E93FF", type: "open" },
      { name: "Aguardando cliente", color: "#f59e0b", type: "open" },
      { name: "Resolvido", color: "#22c55e", type: "won" },
      { name: "Sem retorno", color: "#ef4444", type: "lost" },
    ],
  },
  vendas: {
    label: "Vendas",
    color: "#22c55e",
    cols: [
      { name: "Prospecção", color: "#94a3b8", type: "open" },
      { name: "Proposta", color: "#4E93FF", type: "open" },
      { name: "Negociação", color: "#a855f7", type: "open" },
      { name: "Ganho", color: "#22c55e", type: "won" },
      { name: "Perdido", color: "#ef4444", type: "lost" },
    ],
  },
  suporte: {
    label: "Suporte",
    color: "#f59e0b",
    cols: [
      { name: "Novo ticket", color: "#94a3b8", type: "open" },
      { name: "Em análise", color: "#4E93FF", type: "open" },
      { name: "Aguardando cliente", color: "#f59e0b", type: "open" },
      { name: "Resolvido", color: "#22c55e", type: "won" },
    ],
  },
};

function SortableCard({
  card,
  avatarUrl,
  contactName,
  onOpen,
  onDelete,
}: {
  card: KanbanCard;
  avatarUrl?: string;
  contactName?: string;
  onOpen: (c: KanbanCard) => void;
  onDelete: (c: KanbanCard) => void;
}) {
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
          aria-label="Remover"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </div>
      {card.description && (
        <p className="mt-1 text-xs text-muted-foreground line-clamp-2">{card.description}</p>
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
          Abrir chat
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
  onOpenCard,
  onDeleteCard,
}: {
  column: KanbanColumn;
  cards: KanbanCard[];
  cardMeta: (c: KanbanCard) => { avatarUrl?: string; contactName?: string };
  onAddCard: (colId: string) => void;
  onDeleteColumn: (col: KanbanColumn) => void;
  onOpenCard: (c: KanbanCard) => void;
  onDeleteCard: (c: KanbanCard) => void;
}) {
  const { setNodeRef, isOver } = useDroppable({ id: column.id, data: { type: "column" } });
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
        </div>
        <button
          onClick={() => onDeleteColumn(column)}
          className="text-muted-foreground hover:text-destructive"
          aria-label="Remover coluna"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
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
        Novo cartão
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
  const [dragCard, setDragCard] = useState<KanbanCard | null>(null);
  const [seeded, setSeeded] = useState(false);

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
      toast.success("Board criado");
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  const seedExampleBoards = async () => {
    try {
      const examples: { name: string; color: string; cols: { name: string; color: string; type: "open" | "won" | "lost" }[] }[] = [
        {
          name: "Atendimento",
          color: "#4E93FF",
          cols: [
            { name: "Em andamento", color: "#4E93FF", type: "open" },
            { name: "Aguardando cliente", color: "#f59e0b", type: "open" },
            { name: "Resolvido", color: "#22c55e", type: "won" },
            { name: "Sem retorno", color: "#ef4444", type: "lost" },
          ],
        },
        {
          name: "Vendas",
          color: "#22c55e",
          cols: [
            { name: "Prospecção", color: "#94a3b8", type: "open" },
            { name: "Proposta enviada", color: "#4E93FF", type: "open" },
            { name: "Negociação", color: "#a855f7", type: "open" },
            { name: "Fechado - Ganho", color: "#22c55e", type: "won" },
            { name: "Fechado - Perdido", color: "#ef4444", type: "lost" },
          ],
        },
        {
          name: "Suporte",
          color: "#f59e0b",
          cols: [
            { name: "Novo ticket", color: "#94a3b8", type: "open" },
            { name: "Em análise", color: "#4E93FF", type: "open" },
            { name: "Aguardando cliente", color: "#f59e0b", type: "open" },
            { name: "Resolvido", color: "#22c55e", type: "won" },
          ],
        },
      ];
      for (const ex of examples) {
        const b = await createBoard(ex.name, ex.color, `Board exemplo — ${ex.name}`);
        for (const c of ex.cols) await createColumn(b.id, c.name, c.color, c.type);
      }
      const bs = await loadBoards();
      if (bs.length) setActiveBoardId(bs[0].id);
      toast.success("3 boards de exemplo criados");
    } catch (e) {
      toast.error("Não foi possível criar boards de exemplo: " + (e as Error).message);
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
          toast.error("Board de destino não tem colunas");
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
        toast.success("Cartão movido de board");
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
    const moving = nextCards.find((c) => c.id === activeCard.id)!;
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
            <Plus className="h-4 w-4" /> Novo board
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
              <Trash2 className="h-4 w-4" /> Remover board
            </Button>
          )}
        </div>
        {activeBoardId && (
          <Button size="sm" className="shrink-0" onClick={() => setCreatingColumn(true)}>
            <Plus className="h-4 w-4" /> Nova coluna
          </Button>
        )}
      </div>

      <div className="flex-1 overflow-auto scrollbar-soft p-4">
        {loading && (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" /> Carregando...
          </div>
        )}
        {!loading && !activeBoardId && (
          <div className="flex h-full flex-col items-center justify-center gap-3 text-center">
            <p className="text-muted-foreground">Nenhum board ainda.</p>
            <Button onClick={() => setCreatingBoard(true)}>
              <Plus className="h-4 w-4" /> Criar primeiro board
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
                  onOpenCard={openChat}
                  onDeleteCard={setToDeleteCard}
                />
              ))}
              {!columns.length && (
                <div className="text-sm text-muted-foreground">
                  Nenhuma coluna. Clique em "Nova coluna" para começar.
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

      {/* New board */}
      <Dialog open={creatingBoard} onOpenChange={setCreatingBoard}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Novo board</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <div>
              <label className="text-xs font-medium">Nome</label>
              <Input
                placeholder="Nome do board"
                value={newBoardName}
                onChange={(e) => setNewBoardName(e.target.value)}
                autoFocus
              />
            </div>
            <div>
              <label className="text-xs font-medium">Modelo</label>
              <div className="mt-1 grid grid-cols-3 gap-2">
                {(Object.keys(BOARD_TEMPLATES) as BoardTemplateKey[]).map((k) => {
                  const t = BOARD_TEMPLATES[k];
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
                        <span className="h-2 w-2 rounded-full" style={{ background: t.color }} />
                        <span className="font-medium">{t.label}</span>
                      </div>
                      <p className="mt-1 text-[10px] text-muted-foreground line-clamp-2">
                        {t.cols.map((c) => c.name).join(" · ")}
                      </p>
                    </button>
                  );
                })}
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setCreatingBoard(false)}>
              Cancelar
            </Button>
            <Button onClick={handleCreateBoard}>Criar</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* New column */}
      <Dialog open={creatingColumn} onOpenChange={setCreatingColumn}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Nova coluna</DialogTitle>
          </DialogHeader>
          <Input
            placeholder="Nome da coluna"
            value={newColumnName}
            onChange={(e) => setNewColumnName(e.target.value)}
            autoFocus
          />
          <DialogFooter>
            <Button variant="ghost" onClick={() => setCreatingColumn(false)}>
              Cancelar
            </Button>
            <Button onClick={handleCreateColumn}>Criar</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* New card */}
      <Dialog open={!!creatingCard} onOpenChange={(o) => !o && setCreatingCard(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Novo cartão</DialogTitle>
          </DialogHeader>
          <Input
            placeholder="Título"
            value={newCardTitle}
            onChange={(e) => setNewCardTitle(e.target.value)}
            autoFocus
          />
          <Textarea
            placeholder="Descrição (opcional)"
            value={newCardDesc}
            onChange={(e) => setNewCardDesc(e.target.value)}
            rows={3}
          />
          <DialogFooter>
            <Button variant="ghost" onClick={() => setCreatingCard(null)}>
              Cancelar
            </Button>
            <Button onClick={handleCreateCard}>Criar</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={!!toDeleteBoard}
        onOpenChange={(o) => !o && setToDeleteBoard(null)}
        title="Remover board?"
        description={toDeleteBoard ? `${toDeleteBoard.name} e todos os cartões serão apagados.` : undefined}
        confirmLabel="Remover"
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
        title="Remover coluna?"
        description={toDeleteColumn ? `A coluna ${toDeleteColumn.name} e seus cartões serão apagados.` : undefined}
        confirmLabel="Remover"
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
        title="Remover cartão?"
        confirmLabel="Remover"
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