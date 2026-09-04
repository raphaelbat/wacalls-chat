import { useEffect, useState } from "react";
import { Loader2, KanbanSquare, ExternalLink } from "lucide-react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { cardsByChat, createCard, getBoard, listBoards } from "@/services/kanban";
import type { KanbanBoard, KanbanCard, KanbanColumn } from "@/types/kanban";

// KanbanLinkDialog lets an operator either pick an existing card linked to the
// current chat or create a new one inside any board they own. Boards & columns
// are loaded lazily so the dialog stays light.
export const KanbanLinkDialog = ({
  open,
  onOpenChange,
  sessionId,
  chatJid,
  defaultTitle,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  sessionId: string;
  chatJid: string;
  defaultTitle?: string;
}) => {
  const [boards, setBoards] = useState<KanbanBoard[]>([]);
  const [linked, setLinked] = useState<KanbanCard[]>([]);
  const [loading, setLoading] = useState(false);
  const [boardId, setBoardId] = useState("");
  const [columns, setColumns] = useState<KanbanColumn[]>([]);
  const [columnId, setColumnId] = useState("");
  const [title, setTitle] = useState(defaultTitle ?? "");
  const [description, setDescription] = useState("");

  useEffect(() => {
    if (!open) return;
    setTitle(defaultTitle ?? "");
    setDescription("");
    setLoading(true);
    Promise.all([listBoards(), cardsByChat(sessionId, chatJid)])
      .then(([b, l]) => {
        setBoards(b);
        setLinked(l);
        if (b.length > 0) setBoardId(b[0].id);
      })
      .catch((e) => toast.error((e as Error).message))
      .finally(() => setLoading(false));
  }, [open, sessionId, chatJid, defaultTitle]);

  useEffect(() => {
    if (!boardId) {
      setColumns([]);
      setColumnId("");
      return;
    }
    getBoard(boardId)
      .then((s) => {
        setColumns(s.columns);
        setColumnId(s.columns[0]?.id ?? "");
      })
      .catch(() => setColumns([]));
  }, [boardId]);

  const create = async () => {
    if (!boardId || !columnId || !title.trim()) return;
    try {
      await createCard(boardId, {
        columnId,
        title: title.trim(),
        description,
        sessionId,
        chatJid,
      });
      toast.success("Cartão criado e vinculado");
      onOpenChange(false);
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <KanbanSquare className="h-4 w-4" /> Vincular ao Kanban
          </DialogTitle>
        </DialogHeader>

        {loading ? (
          <div className="flex justify-center py-6">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : boards.length === 0 ? (
          <div className="space-y-2 rounded-md border p-4 text-sm">
            Nenhum quadro encontrado.
            <Link to="/kanban" className="block text-primary underline">
              Criar um quadro
            </Link>
          </div>
        ) : (
          <div className="space-y-4">
            {linked.length > 0 && (
              <div className="rounded-md border bg-muted/30 p-2 text-xs">
                <div className="mb-1 font-medium">Já vinculados a este atendimento:</div>
                <ul className="space-y-1">
                  {linked.map((c) => (
                    <li key={c.id} className="flex items-center justify-between">
                      <span className="truncate">{c.title}</span>
                      <Link to={`/kanban/${c.boardId}`} className="flex items-center gap-1 text-primary">
                        Abrir <ExternalLink className="h-3 w-3" />
                      </Link>
                    </li>
                  ))}
                </ul>
              </div>
            )}

            <div className="grid grid-cols-2 gap-3">
              <div>
                <Label>Quadro</Label>
                <select
                  value={boardId}
                  onChange={(e) => setBoardId(e.target.value)}
                  className="h-9 w-full rounded-md border bg-background px-2 text-sm"
                >
                  {boards.map((b) => (
                    <option key={b.id} value={b.id}>{b.name}</option>
                  ))}
                </select>
              </div>
              <div>
                <Label>Coluna</Label>
                <select
                  value={columnId}
                  onChange={(e) => setColumnId(e.target.value)}
                  className="h-9 w-full rounded-md border bg-background px-2 text-sm"
                >
                  {columns.map((c) => (
                    <option key={c.id} value={c.id}>{c.name}</option>
                  ))}
                </select>
              </div>
            </div>
            <div>
              <Label>Título</Label>
              <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Resumo do atendimento" />
            </div>
            <div>
              <Label>Descrição</Label>
              <Textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={3} />
            </div>
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>Fechar</Button>
          <Button onClick={create} disabled={!boardId || !columnId || !title.trim()}>
            Criar cartão
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};