// Local-only plan flag for WhatsApp instances. Until a real billing backend
// is wired, we mark instances as "free" or "paid" via localStorage. The new
// connections list shows a "Pagar para Conectar" badge for paid instances
// that haven't been activated yet.
const KEY = "wacalls.instance.plan.v1";

export type InstancePlan = "free" | "paid";

type PlanMap = Record<string, { plan: InstancePlan; price: number; paid?: boolean }>;

const load = (): PlanMap => {
  try {
    const raw = localStorage.getItem(KEY);
    return raw ? (JSON.parse(raw) as PlanMap) : {};
  } catch {
    return {};
  }
};

const save = (m: PlanMap) => {
  try {
    localStorage.setItem(KEY, JSON.stringify(m));
  } catch {
    /* noop */
  }
};

export const getInstancePlan = (id: string) =>
  load()[id] ?? { plan: "free" as InstancePlan, price: 0, paid: true };

export const setInstancePlan = (id: string, plan: InstancePlan, price = 49.9) => {
  const m = load();
  m[id] = { plan, price, paid: plan === "free" ? true : m[id]?.paid ?? false };
  save(m);
};

export const markInstancePaid = (id: string) => {
  const m = load();
  const cur = m[id] ?? { plan: "paid" as InstancePlan, price: 49.9 };
  m[id] = { ...cur, paid: true };
  save(m);
};

export const formatBRL = (v: number) =>
  v.toLocaleString("pt-BR", { style: "currency", currency: "BRL" });
