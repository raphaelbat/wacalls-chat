// Local-only metadata for contacts (email + cadastro date) — the backend
// doesn't track these today, so we persist them per browser keyed by
// sessionId/chatJid. Used by /contacts edit dialog and CSV export/import.

const KEY = "contact-meta:v1";

export type ContactMeta = {
  email?: string;
  createdAt?: number; // unix seconds
};

type Store = Record<string, ContactMeta>;

const id = (sessionId: string, chatJid: string) => `${sessionId}::${chatJid}`;

const read = (): Store => {
  try {
    const raw = localStorage.getItem(KEY);
    return raw ? (JSON.parse(raw) as Store) : {};
  } catch {
    return {};
  }
};

const write = (s: Store) => {
  try {
    localStorage.setItem(KEY, JSON.stringify(s));
  } catch {
    /* ignore quota */
  }
};

export const getContactMeta = (sessionId: string, chatJid: string): ContactMeta => {
  return read()[id(sessionId, chatJid)] ?? {};
};

export const setContactMeta = (
  sessionId: string,
  chatJid: string,
  patch: ContactMeta,
): ContactMeta => {
  const s = read();
  const k = id(sessionId, chatJid);
  const next = { ...(s[k] ?? {}), ...patch };
  s[k] = next;
  write(s);
  return next;
};

export const ensureCreatedAt = (
  sessionId: string,
  chatJid: string,
  fallback?: number,
): number => {
  const cur = getContactMeta(sessionId, chatJid);
  if (cur.createdAt && cur.createdAt > 0) return cur.createdAt;
  const ts = fallback && fallback > 0 ? fallback : Math.floor(Date.now() / 1000);
  setContactMeta(sessionId, chatJid, { createdAt: ts });
  return ts;
};

export const dumpContactMeta = (): Store => read();

export const mergeContactMeta = (entries: Array<{ sessionId: string; chatJid: string; meta: ContactMeta }>) => {
  const s = read();
  for (const e of entries) {
    const k = id(e.sessionId, e.chatJid);
    s[k] = { ...(s[k] ?? {}), ...e.meta };
  }
  write(s);
};