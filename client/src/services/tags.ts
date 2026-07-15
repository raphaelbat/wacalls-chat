import { apiGet, apiPost, apiDelete } from "@/lib/api";
import { apiUrl } from "@/lib/api-base";
import { getClientId } from "@/lib/client-id";
import type { Tag } from "@/types/tag";

export const listTags = () =>
  apiGet<{ tags: Tag[] }>("/api/tags").then((r) => r.tags ?? []);

export const createTag = (name: string, color: string) =>
  apiPost<Tag>("/api/tags", { name, color });

export const deleteTag = (id: string) => apiDelete(`/api/tags/${id}`);

export const updateTag = async (id: string, name: string, color: string): Promise<void> => {
  const r = await fetch(apiUrl(`/api/tags/${id}`), {
    method: "PUT",
    headers: { "X-Client-Id": getClientId(), "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify({ name, color }),
  });
  if (!r.ok) throw new Error(`update tag ${r.status}`);
};

const enc = (s: string) => encodeURIComponent(s);

export const listChatTags = (sessionId: string, chatJid: string) =>
  apiGet<{ tags: Tag[] }>(`/api/sessions/${enc(sessionId)}/chats/${enc(chatJid)}/tags`).then(
    (r) => r.tags ?? [],
  );

export const attachChatTag = (sessionId: string, chatJid: string, tagId: string) =>
  apiPost<{ tags: Tag[] }>(
    `/api/sessions/${enc(sessionId)}/chats/${enc(chatJid)}/tags`,
    { tagId },
  ).then((r) => r.tags ?? []);

export const detachChatTag = (sessionId: string, chatJid: string, tagId: string) =>
  apiDelete(`/api/sessions/${enc(sessionId)}/chats/${enc(chatJid)}/tags/${enc(tagId)}`);
