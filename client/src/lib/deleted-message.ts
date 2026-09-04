import type { ChatMessage } from "@/types/chat";

// Textos com que os backends entregam uma mensagem revogada (apagada). Alguns
// mandam o placeholder localizado, outros mandam kind="unknown" com corpo
// "Mensagem" ou vazio.
const DELETED_TEXTS = new Set([
  "mensagem",
  "esta mensagem foi apagada",
  "voce apagou esta mensagem",
  "você apagou esta mensagem",
  "this message was deleted",
  "you deleted this message",
]);

/** Heurística compartilhada, a partir do tipo e do corpo da mensagem. */
export const looksDeleted = (kind: string, body?: string): boolean => {
  const t = (body || "").trim().toLowerCase();
  if (kind === "text") return DELETED_TEXTS.has(t);
  if (kind === "unknown") return t === "" || DELETED_TEXTS.has(t);
  return false;
};

/** Esta mensagem é o placeholder de uma apagada? */
export const isDeletedPlaceholder = (message: ChatMessage): boolean =>
  looksDeleted(message.kind, message.body);

/** True quando a mensagem foi apagada, pela flag do backend ou pela heurística. */
export const isDeletedMessage = (message: ChatMessage): boolean =>
  !!message.deleted || isDeletedPlaceholder(message);
