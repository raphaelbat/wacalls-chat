const KEY = "chat:msg:history:v1";
const MAX = 80;

function read(): string[] {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return [];
    const arr = JSON.parse(raw);
    return Array.isArray(arr) ? arr.filter((x) => typeof x === "string") : [];
  } catch {
    return [];
  }
}

function write(list: string[]) {
  try {
    localStorage.setItem(KEY, JSON.stringify(list.slice(0, MAX)));
  } catch {
    /* ignore */
  }
}

export function rememberMessage(text: string) {
  const v = text.trim();
  if (!v || v.length < 3 || v.length > 500) return;
  const list = read().filter((x) => x.toLowerCase() !== v.toLowerCase());
  list.unshift(v);
  write(list);
}

export function suggestMessages(query: string, limit = 5): string[] {
  const q = query.trim().toLowerCase();
  if (q.length < 2) return [];
  const list = read();
  const out: string[] = [];
  for (const item of list) {
    const lc = item.toLowerCase();
    if (lc === q) continue;
    if (lc.includes(q)) out.push(item);
    if (out.length >= limit) break;
  }
  return out;
}