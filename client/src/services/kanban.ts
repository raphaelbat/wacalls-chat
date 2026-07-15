import { apiGet, apiPost, apiDelete } from "@/lib/api";
import { apiUrl } from "@/lib/api-base";
import { getClientId } from "@/lib/client-id";
import type {
  BoardSnapshot, KanbanAutomation, KanbanBoard, KanbanCard, KanbanColumn,
  StageType,
} from "@/types/kanban";

const put = async (path: string, body: unknown): Promise<void> => {
  const r = await fetch(apiUrl(path), {
    method: "PUT",
    headers: { "X-Client-Id": getClientId(), "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify(body),
  });
  if (!r.ok) throw new Error(`${path} ${r.status}`);
};

// Boards
export const listBoards = () =>
  apiGet<{ boards: KanbanBoard[] }>("/api/kanban/boards").then((r) => r.boards ?? []);
export const getBoard = (id: string) =>
  apiGet<BoardSnapshot>(`/api/kanban/boards/${id}`);
export const createBoard = (name: string, color: string, description = "") =>
  apiPost<KanbanBoard>("/api/kanban/boards", { name, color, description });
export const updateBoard = (id: string, name: string, color: string, description: string) =>
  put(`/api/kanban/boards/${id}`, { name, color, description });
export const deleteBoard = (id: string) => apiDelete(`/api/kanban/boards/${id}`);

// Columns
export const createColumn = (boardId: string, name: string, color: string, stageType: StageType = "open") =>
  apiPost<KanbanColumn>(`/api/kanban/boards/${boardId}/columns`, { name, color, stageType });
export const updateColumn = (id: string, name: string, color: string, stageType: StageType = "open") =>
  put(`/api/kanban/columns/${id}`, { name, color, stageType });
export const deleteColumn = (id: string) => apiDelete(`/api/kanban/columns/${id}`);

// Cards
export type CreateCardInput = {
  columnId: string;
  title: string;
  description?: string;
  color?: string;
  sessionId?: string;
  chatJid?: string;
  assigneeId?: string;
  dueAt?: number;
};
export const createCard = (boardId: string, input: CreateCardInput) =>
  apiPost<KanbanCard>(`/api/kanban/boards/${boardId}/cards`, input);

export type UpdateCardInput = Partial<{
  title: string;
  description: string;
  color: string;
  assigneeId: string;
  chatJid: string;
  sessionId: string;
  dueAt: number;
}>;
export const updateCard = (id: string, input: UpdateCardInput) =>
  put(`/api/kanban/cards/${id}`, input);

export const moveCard = async (id: string, columnId: string, position: number): Promise<void> => {
  const r = await fetch(apiUrl(`/api/kanban/cards/${id}/move`), {
    method: "POST",
    headers: { "X-Client-Id": getClientId(), "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify({ columnId, position }),
  });
  if (!r.ok) throw new Error(`move card ${r.status}`);
};

export const deleteCard = (id: string) => apiDelete(`/api/kanban/cards/${id}`);

export const cardsByChat = (sid: string, jid: string) =>
  apiGet<{ cards: KanbanCard[] }>(`/api/kanban/cards/by-chat?sid=${encodeURIComponent(sid)}&jid=${encodeURIComponent(jid)}`).then(
    (r) => r.cards ?? [],
  );

// Automations -------------------------------------------------------------
export const listAutomations = (boardId: string) =>
  apiGet<{ automations: KanbanAutomation[] }>(`/api/kanban/boards/${boardId}/automations`)
    .then((r) => r.automations ?? []);

export const upsertAutomation = (boardId: string, body: Partial<KanbanAutomation>) =>
  apiPost<KanbanAutomation>(`/api/kanban/boards/${boardId}/automations`, body);

export const deleteAutomation = (id: string) =>
  apiDelete(`/api/kanban/automations/${id}`);