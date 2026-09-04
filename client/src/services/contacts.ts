import { apiUrl } from "@/lib/api-base";
import { getClientId } from "@/lib/client-id";

export interface ContactRow {
  sessionId: string;
  sessionName: string;
  chatJid: string;
  name: string;
  phone: string;
  avatarUrl?: string;
  isGroup: boolean;
  lastTs: number;
  lastMessage?: string;
  unread: number;
}

export interface ContactListResponse {
  contacts: ContactRow[];
  total: number;
  limit: number;
  offset: number;
}

export interface ListContactsOpts {
  q?: string;
  kind?: "" | "user" | "group";
  limit?: number;
  offset?: number;
}

// Aggregated list of every conversation seen across the user's WhatsApp
// sessions. Used by the dedicated /contacts page (search + pagination).
export const listContacts = async (opts: ListContactsOpts = {}): Promise<ContactListResponse> => {
  const qs = new URLSearchParams();
  if (opts.q) qs.set("q", opts.q);
  if (opts.kind) qs.set("kind", opts.kind);
  qs.set("limit", String(opts.limit ?? 50));
  qs.set("offset", String(opts.offset ?? 0));
  const res = await fetch(apiUrl(`/api/contacts?${qs.toString()}`), { credentials: "include" });
  if (!res.ok) {
    const t = await res.text().catch(() => "");
    throw new Error(`${res.status} ${t}`);
  }
  return (await res.json()) as ContactListResponse;
};

const headersJSON = (): HeadersInit => ({
  "Content-Type": "application/json",
  "X-Client-Id": getClientId(),
});

const headersMultipart = (): HeadersInit => ({ "X-Client-Id": getClientId() });

const parseError = async (r: Response): Promise<never> => {
  let msg = `${r.status}`;
  try {
    const j = await r.json();
    if (j?.error) msg = String(j.error);
  } catch {
    /* ignore */
  }
  throw new Error(msg);
};

export interface CreateContactInput {
  sessionId: string;
  phone: string;
  name: string;
  avatar?: File | null;
}

export const createContact = async (input: CreateContactInput): Promise<ContactRow> => {
  let res: Response;
  if (input.avatar) {
    const fd = new FormData();
    fd.set("sessionId", input.sessionId);
    fd.set("phone", input.phone);
    fd.set("name", input.name);
    fd.set("file", input.avatar);
    res = await fetch(apiUrl("/api/contacts"), {
      method: "POST",
      credentials: "include",
      headers: headersMultipart(),
      body: fd,
    });
  } else {
    res = await fetch(apiUrl("/api/contacts"), {
      method: "POST",
      credentials: "include",
      headers: headersJSON(),
      body: JSON.stringify({
        sessionId: input.sessionId,
        phone: input.phone,
        name: input.name,
      }),
    });
  }
  if (!res.ok) return parseError(res);
  return (await res.json()) as ContactRow;
};

export interface UpdateContactInput {
  name?: string;
  avatar?: File | null;
  clearAvatar?: boolean;
}

export const updateContact = async (
  sessionId: string,
  chatJid: string,
  input: UpdateContactInput,
): Promise<void> => {
  const url = apiUrl(`/api/contacts/${encodeURIComponent(sessionId)}/${encodeURIComponent(chatJid)}`);
  let res: Response;
  if (input.avatar) {
    const fd = new FormData();
    if (input.name !== undefined) fd.set("name", input.name);
    if (input.clearAvatar) fd.set("clearAvatar", "1");
    fd.set("file", input.avatar);
    res = await fetch(url, {
      method: "PATCH",
      credentials: "include",
      headers: headersMultipart(),
      body: fd,
    });
  } else {
    res = await fetch(url, {
      method: "PATCH",
      credentials: "include",
      headers: headersJSON(),
      body: JSON.stringify({ name: input.name, clearAvatar: !!input.clearAvatar }),
    });
  }
  if (!res.ok) return parseError(res);
};

export const deleteContact = async (sessionId: string, chatJid: string): Promise<void> => {
  const res = await fetch(
    apiUrl(`/api/contacts/${encodeURIComponent(sessionId)}/${encodeURIComponent(chatJid)}`),
    { method: "DELETE", credentials: "include", headers: headersMultipart() },
  );
  if (!res.ok && res.status !== 204) return parseError(res);
};