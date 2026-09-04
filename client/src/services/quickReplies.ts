import { apiGet, apiPost } from "@/lib/api";
import { apiUrl } from "@/lib/api-base";
import { getClientId } from "@/lib/client-id";

/** Tipo do anexo de uma resposta rápida — o mesmo vocabulário do envio de mídia no chat. */
export type QuickReplyKind = "image" | "video" | "audio" | "document";

export type QuickReply = {
  id: string;
  shortcut: string;
  title: string;
  content: string;
  global: boolean;
  ownerId?: string;
  usedCount: number;
  createdAt: number;
  /** URL do anexo servida pelo backend (/api/media/quickreplies/...), quando houver. */
  mediaUrl?: string;
  mediaName?: string;
  mediaMime?: string;
  mediaKind?: QuickReplyKind;
  mediaSize?: number;
};

export type QuickReplyInput = {
  shortcut: string;
  title: string;
  content: string;
  global: boolean;
};

const send = async (path: string, method: string, body?: unknown) => {
  const r = await fetch(apiUrl(path), {
    method,
    credentials: "include",
    headers: { "X-Client-Id": getClientId(), "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!r.ok) throw new Error(`${method} ${path} ${r.status}`);
};

export const listQuickReplies = () =>
  apiGet<{ quickReplies: QuickReply[] }>("/api/quick-replies").then((r) => r.quickReplies ?? []);

export const createQuickReply = (input: QuickReplyInput) =>
  apiPost<QuickReply>("/api/quick-replies", input);

export const updateQuickReply = (id: string, input: QuickReplyInput) =>
  send(`/api/quick-replies/${id}`, "PUT", input);

export const deleteQuickReply = (id: string) => send(`/api/quick-replies/${id}`, "DELETE");

export const markQuickReplyUsed = (id: string) =>
  send(`/api/quick-replies/${id}/used`, "POST").catch(() => undefined);

/** Deduz o tipo do anexo pelo MIME do arquivo escolhido. */
export const quickReplyKindOf = (file: File): QuickReplyKind => {
  const t = (file.type || "").toLowerCase();
  if (t.startsWith("image/")) return "image";
  if (t.startsWith("video/")) return "video";
  if (t.startsWith("audio/")) return "audio";
  return "document";
};

/** Anexa (ou substitui) o arquivo da resposta rápida e devolve a linha atualizada. */
export const uploadQuickReplyMedia = async (id: string, file: File): Promise<QuickReply> => {
  const fd = new FormData();
  fd.append("file", file);
  fd.append("kind", quickReplyKindOf(file));
  fd.append("filename", file.name || "arquivo");
  const r = await fetch(apiUrl(`/api/quick-replies/${id}/media`), {
    method: "POST",
    credentials: "include",
    headers: { "X-Client-Id": getClientId() },
    body: fd,
  });
  if (!r.ok) {
    const t = await r.text().catch(() => "");
    throw new Error(`upload ${r.status} ${t}`);
  }
  return (await r.json()) as QuickReply;
};

/** Remove só o anexo, preservando o texto do snippet. */
export const removeQuickReplyMedia = (id: string) => send(`/api/quick-replies/${id}/media`, "DELETE");

/**
 * Baixa o anexo salvo e devolve um File pronto para o envio no chat, que
 * reaproveita o mesmo endpoint de mídia usado pelo botão de anexo.
 */
export const fetchQuickReplyFile = async (qr: QuickReply): Promise<File> => {
  if (!qr.mediaUrl) throw new Error("quick reply sem anexo");
  const r = await fetch(apiUrl(qr.mediaUrl), {
    credentials: "include",
    headers: { "X-Client-Id": getClientId() },
  });
  if (!r.ok) throw new Error(`download ${r.status}`);
  const blob = await r.blob();
  return new File([blob], qr.mediaName || "arquivo", {
    type: qr.mediaMime || blob.type || "application/octet-stream",
  });
};