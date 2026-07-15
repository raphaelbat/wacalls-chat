import type { FlowRow } from "@/types/flow";

const TRIGGER_LABEL: Record<string, string> = {
  inbound: "Chamada recebida",
  outbound: "Chamada feita",
  manual: "Manual",
  message: "Mensagem",
};

export const triggerLabel = (t: string): string => TRIGGER_LABEL[t] ?? t;

export const flowOptionLabel = (f: FlowRow): string => {
  const tag = triggerLabel(f.trigger);
  const suffix = f.enabled ? "" : " · desativado";
  return `${f.name} — ${tag}${suffix}`;
};