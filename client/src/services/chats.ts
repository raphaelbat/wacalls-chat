import type { ChatClosure, ChatEvent, ChatMessage, ChatMeta, ChatSummary } from "@/types/chat";
import { apiUrl } from "@/lib/api-base";

async function http<T>(path: string, init?: RequestInit): Promise<T> {
  const url = apiUrl(path);
  let res: Response;
  try {
    res = await fetch(url, {
      ...init,
      credentials: "include",
      headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
    });
  } catch (err) {
    // eslint-disable-next-line no-console
    console.error("[chats] network error", url, err);
    throw err;
  }
  if (!res.ok) {
    const body = await res.text().catch(() => "");
    // eslint-disable-next-line no-console
    console.error("[chats] HTTP", res.status, url, body);
    throw new Error(`${res.status} ${body}`);
  }
  return res.json() as Promise<T>;
}

export const listChats = (sessionId: string) =>
  http<{ chats: ChatSummary[] }>(`/api/sessions/${sessionId}/chats`).then((r) => r.chats ?? []);

// resolveLidPhone asks the backend to translate a WhatsApp LID
// ("199347350294740@lid", an internal hidden-user ID) into the real
// E.164 phone-number digits. Returns null when WhatsApp has not yet
// exposed the mapping for this contact.
export const resolveLidPhone = async (
  sessionId: string,
  jid: string,
): Promise<{ phone: string; jid: string } | null> => {
  try {
    return await http<{ phone: string; jid: string }>(
      `/api/sessions/${sessionId}/lid-pn/${encodeURIComponent(jid)}`,
    );
  } catch {
    return null;
  }
};

export const listMessages = (sessionId: string, jid: string, opts: { limit?: number; before?: number } = {}) => {
  const q = new URLSearchParams();
  if (opts.limit) q.set("limit", String(opts.limit));
  if (opts.before) q.set("before", String(opts.before));
  const qs = q.toString() ? `?${q.toString()}` : "";
  return http<{ messages: ChatMessage[] }>(`/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/messages${qs}`).then(
    (r) => r.messages ?? [],
  );
};

export const sendMessage = (sessionId: string, jid: string, text: string) =>
  http<{ message: ChatMessage }>(`/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/send`, {
    method: "POST",
    body: JSON.stringify({ text }),
  }).then((r) => r.message);

export const assignChat = (sessionId: string, jid: string) =>
  http<{ ok: string }>(`/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/assign`, { method: "POST" });

export const closeChat = (sessionId: string, jid: string, reason?: string) =>
  http<{ ok: string }>(`/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/close`, {
    method: "POST",
    body: JSON.stringify({ reason: reason ?? "" }),
  });

// requeueChat moves the conversation back into the waiting queue and
// unassigns the current agent. Used by the "Devolver para fila" button.
export const requeueChat = (sessionId: string, jid: string) =>
  http<{ ok: string }>(`/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/requeue`, {
    method: "POST",
  });

// transferChat hands the conversation over to another operator.
export const transferChat = (sessionId: string, jid: string, userId: string) =>
  http<{ ok: string }>(`/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/transfer`, {
    method: "POST",
    body: JSON.stringify({ userId }),
  });

// assignChatTo routes a freshly-opened conversation to a specific operator
// and/or queue. Either field may be empty (chat lands in the waiting queue
// with no owner if both are empty).
export const assignChatTo = (
  sessionId: string,
  jid: string,
  payload: { userId?: string; queueId?: string },
) =>
  http<{ ok: string }>(`/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/assign-to`, {
    method: "POST",
    body: JSON.stringify({ userId: payload.userId ?? "", queueId: payload.queueId ?? "" }),
  });

export interface OperatorRef {
  id: string;
  email: string;
  name?: string;
  companyName?: string;
}

export const listOperators = () =>
  http<{ operators: OperatorRef[] }>(`/api/operators`).then((r) => r.operators ?? []);

export const markChatRead = (sessionId: string, jid: string, ts?: number) =>
  http<{ meta: { lastReadTs: number } }>(`/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/read`, {
    method: "POST",
    body: JSON.stringify({ ts: ts ?? 0 }),
  });

export const listChatClosures = (sessionId: string, jid: string) =>
  http<{ closures: ChatClosure[] }>(`/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/closures`).then((r) => r.closures);

// listChatEvents returns the lifecycle audit timeline (created/opened/closed/...)
// in chronological order. Rendered inline as system pills inside the chat.
export const listChatEvents = (sessionId: string, jid: string) =>
  http<{ events: ChatEvent[] }>(`/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/events`).then((r) => r.events ?? []);

// syncChatContact forces the backend to refresh name + avatar of a single
// conversation (calls WhatsApp APIs). Returns the updated chat meta.
export const syncChatContact = (sessionId: string, jid: string) =>
  http<{ meta: ChatMeta }>(`/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/sync`, { method: "POST" }).then(
    (r) => r.meta,
  );

// syncAllChatContacts kicks off a background refresh for every known chat in
// the session. Returns the number of conversations queued.
export const syncAllChatContacts = (sessionId: string) =>
  http<{ queued: number }>(`/api/sessions/${sessionId}/chats/sync-all`, { method: "POST" });

// Per-message moderation. delete = revoke for everyone (only own messages).
export const deleteMessage = (sessionId: string, jid: string, mid: string) =>
  http<{ ok: string }>(
    `/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/messages/${encodeURIComponent(mid)}/delete`,
    { method: "POST" },
  );

export const editMessage = (sessionId: string, jid: string, mid: string, text: string) =>
  http<{ message: ChatMessage }>(
    `/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/messages/${encodeURIComponent(mid)}/edit`,
    { method: "POST", body: JSON.stringify({ text }) },
  ).then((r) => r.message);

export const forwardMessage = (sessionId: string, jid: string, mid: string, targets: string[]) =>
  http<{ sent: number; errors: string[] }>(
    `/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/messages/${encodeURIComponent(mid)}/forward`,
    { method: "POST", body: JSON.stringify({ targets }) },
  );

export const sendContact = (sessionId: string, jid: string, name: string, phone: string) =>
  http<{ message: ChatMessage }>(`/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/contact`, {
    method: "POST",
    body: JSON.stringify({ name, phone }),
  }).then((r) => r.message);

export const sendMedia = async (
  sessionId: string,
  jid: string,
  file: File,
  kind: "image" | "video" | "audio" | "document",
  caption = "",
): Promise<ChatMessage> => {
  const fd = new FormData();
  fd.append("file", file);
  fd.append("kind", kind);
  if (caption) fd.append("caption", caption);
  fd.append("filename", file.name || "arquivo");
  const res = await fetch(apiUrl(`/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/media`), {
    method: "POST",
    credentials: "include",
    body: fd,
  });
  if (!res.ok) {
    const t = await res.text().catch(() => "");
    throw new Error(`${res.status} ${t}`);
  }
  const j = (await res.json()) as { message: ChatMessage };
  return j.message;
};

// sendMediaWithProgress uploads using XHR so the caller can render an upload
// progress bar. Falls back gracefully when progress events aren't emitted.
export const sendMediaWithProgress = (
  sessionId: string,
  jid: string,
  file: File,
  kind: "image" | "video" | "audio" | "document",
  caption: string,
  onProgress?: (pct: number) => void,
): Promise<ChatMessage> =>
  new Promise((resolve, reject) => {
    const fd = new FormData();
    fd.append("file", file);
    fd.append("kind", kind);
    if (caption) fd.append("caption", caption);
    fd.append("filename", file.name || "arquivo");
    const xhr = new XMLHttpRequest();
    xhr.open("POST", apiUrl(`/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/media`));
    xhr.withCredentials = true;
    xhr.upload.onprogress = (ev) => {
      if (!onProgress) return;
      if (ev.lengthComputable) onProgress(Math.round((ev.loaded / ev.total) * 100));
    };
    xhr.onerror = () => reject(new Error("Falha de rede ao enviar"));
    xhr.onload = () => {
      if (xhr.status < 200 || xhr.status >= 300) {
        reject(new Error(`${xhr.status} ${xhr.responseText || ""}`));
        return;
      }
      try {
        const j = JSON.parse(xhr.responseText) as { message: ChatMessage };
        onProgress?.(100);
        resolve(j.message);
      } catch (e) {
        reject(e as Error);
      }
    };
    xhr.send(fd);
  });

export const getSignature = () =>
  http<{ enabled: boolean; text: string }>(`/api/me/signature`);

export const setSignature = (enabled: boolean, text: string) =>
  http<{ enabled: boolean; text: string }>(`/api/me/signature`, {
    method: "PUT",
    body: JSON.stringify({ enabled, text }),
  });

// sendNote stores an internal/private annotation on the chat. The note is
// never delivered to the WhatsApp peer — only operators see it in the chat
// timeline.
export const sendNote = (sessionId: string, jid: string, text: string) =>
  http<{ message: ChatMessage }>(`/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/note`, {
    method: "POST",
    body: JSON.stringify({ text }),
  }).then((r) => r.message);

// triggerFlow manually kicks off a flow against the current chat context.
export const triggerFlow = (sessionId: string, jid: string, flowId: string) =>
  http<{ ok: string; flowId: string }>(`/api/sessions/${sessionId}/chats/${encodeURIComponent(jid)}/trigger-flow`, {
    method: "POST",
    body: JSON.stringify({ flowId }),
  });