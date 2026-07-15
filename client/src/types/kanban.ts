export type KanbanBoard = {
  id: string;
  name: string;
  color: string;
  description: string;
  ownerId?: string;
  createdAt: number;
};

export type StageType = "open" | "won" | "lost";

export type KanbanColumn = {
  id: string;
  boardId: string;
  name: string;
  color: string;
  position: number;
  stageType: StageType;
  createdAt: number;
};

export type KanbanCard = {
  id: string;
  boardId: string;
  columnId: string;
  title: string;
  description: string;
  color: string;
  position: number;
  sessionId?: string;
  chatJid?: string;
  assigneeId?: string;
  dueAt?: number;
  createdAt: number;
  updatedAt: number;
};

export type AutomationKind = "replied" | "answered_call" | "new_contact";

export type KanbanAutomation = {
  id: string;
  boardId: string;
  kind: AutomationKind;
  whenStageId: string;
  targetStageId: string;
  enabled: boolean;
  createdAt: number;
};

export type BoardSnapshot = {
  board: KanbanBoard;
  columns: KanbanColumn[];
  cards: KanbanCard[];
  automations?: KanbanAutomation[];
};