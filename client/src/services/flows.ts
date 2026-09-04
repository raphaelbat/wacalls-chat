import type { FlowGraph, FlowRow, FlowRun, FlowRunEvent } from "@/types/flow";
import { apiUrl } from "@/lib/api-base";

async function http<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(apiUrl(path), {
    ...init,
    credentials: "include",
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
  });
  if (!res.ok) throw new Error(`${res.status} ${await res.text()}`);
  return res.json();
}

export const listFlows = () => http<{ flows: FlowRow[] }>(`/api/flows`).then((r) => r.flows ?? []);
export const getFlow = (id: string) => http<FlowRow>(`/api/flows/${id}`);
export const createFlow = (body: Partial<FlowRow> = {}) =>
  http<FlowRow>(`/api/flows`, { method: "POST", body: JSON.stringify(body) });
export const updateFlow = (id: string, body: Partial<FlowRow>) =>
  http<FlowRow>(`/api/flows/${id}`, { method: "PUT", body: JSON.stringify(body) });
export const deleteFlow = (id: string) => http<{ ok: string }>(`/api/flows/${id}`, { method: "DELETE" });
export const duplicateFlow = (id: string) => http<FlowRow>(`/api/flows/${id}/duplicate`, { method: "POST" });
export const testFlow = (id: string, inputs: Record<string, unknown> = {}) =>
  http<{ trace: string }>(`/api/flows/${id}/test`, { method: "POST", body: JSON.stringify({ inputs }) });
export const listRuns = (id: string) => http<{ runs: FlowRun[] }>(`/api/flows/${id}/runs`).then((r) => r.runs ?? []);
export const listRunEvents = (runId: string) =>
  http<{ events: FlowRunEvent[] }>(`/api/runs/${runId}/events`).then((r) => r.events ?? []);

export interface FlowStats {
  executions: number;
  completed: number;
  interacted: number;
  avgNodes: number;
  completionPct: number;
  interactPct: number;
}
export const getFlowStats = (id: string) =>
  http<{ flowId: string; name: string; keywords: string; stats: FlowStats }>(`/api/flows/${id}/stats`);

export const serializeGraph = (g: FlowGraph) => JSON.stringify(g);
export const parseGraph = (s: string | undefined | null): FlowGraph => {
  if (!s) return { nodes: [], edges: [], startNodeId: "" };
  try {
    const v = JSON.parse(s) as FlowGraph;
    return {
      nodes: v.nodes ?? [],
      edges: v.edges ?? [],
      startNodeId: v.startNodeId ?? "",
      kind: v.kind,
      voice: v.voice,
    };
  } catch {
    return { nodes: [], edges: [], startNodeId: "" };
  }
};