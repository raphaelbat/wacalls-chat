const KEY = "wacalls.clientId";

const generate = (): string => {
  if (typeof crypto.randomUUID === "function") return crypto.randomUUID();
  return "c-" + Math.random().toString(36).slice(2) + Date.now().toString(36);
};

export const getClientId = (): string => {
  let id = localStorage.getItem(KEY);
  if (!id) {
    id = generate();
    localStorage.setItem(KEY, id);
  }
  return id;
};
