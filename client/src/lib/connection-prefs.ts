// Per-session UI preferences persisted in localStorage. These mirror the
// toggles shown on the /connections page so the choices survive reloads and
// are visible to other components (Dialer, CallAutoRecorder, etc.).
const RECORD_KEY = "wacalls.connection.recordCalls";
const RECEIVE_KEY = "wacalls.connection.receiveCalls";

type Map = Record<string, boolean>;

const readMap = (key: string): Map => {
  if (typeof window === "undefined") return {};
  try {
    const raw = window.localStorage.getItem(key);
    if (!raw) return {};
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === "object" ? (parsed as Map) : {};
  } catch {
    return {};
  }
};

const writeMap = (key: string, m: Map) => {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(key, JSON.stringify(m));
  } catch {}
};

export const getRecordCalls = (sid: string): boolean => {
  const m = readMap(RECORD_KEY);
  // Default ON so attendant/dialer calls are recorded automatically and
  // appear in /history → Detalhes without manual toggling.
  return sid in m ? !!m[sid] : true;
};
export const setRecordCalls = (sid: string, on: boolean): void => {
  const m = readMap(RECORD_KEY);
  // Persist both states explicitly so the new default (on) can be
  // overridden per connection.
  m[sid] = on;
  writeMap(RECORD_KEY, m);
  if (typeof window !== "undefined") {
    window.dispatchEvent(new CustomEvent("wacalls:record-pref-changed", { detail: { sid, on } }));
  }
};

export const getReceiveCalls = (sid: string): boolean => {
  const m = readMap(RECEIVE_KEY);
  // default true (mirror the on-screen default)
  return sid in m ? !!m[sid] : true;
};
export const setReceiveCalls = (sid: string, on: boolean): void => {
  const m = readMap(RECEIVE_KEY);
  m[sid] = on;
  writeMap(RECEIVE_KEY, m);
};