import { apiGet, apiPost, apiDelete } from "@/lib/api";
import { apiUrl } from "@/lib/api-base";
import { getClientId } from "@/lib/client-id";
import type { Queue } from "@/types/queue";

export const listQueues = () =>
  apiGet<{ queues: Queue[] }>("/api/queues").then((r) => r.queues ?? []);

export const createQueue = (name: string, color: string) =>
  apiPost<Queue>("/api/queues", { name, color });

export const deleteQueue = (id: string) => apiDelete(`/api/queues/${id}`);

export type QueueExtrasPayload = {
  orderBot?: string;
  closeTicket?: boolean;
  rotation?: boolean;
  rotationInterval?: string;
  rotationMode?: string;
  autoRandomize?: boolean;
  agentId?: string;
  greeting?: string;
};

export const updateQueue = async (
  id: string,
  name: string,
  color: string,
  extras: QueueExtrasPayload = {},
): Promise<void> => {
  const r = await fetch(apiUrl(`/api/queues/${id}`), {
    method: "PUT",
    headers: { "X-Client-Id": getClientId(), "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify({ name, color, ...extras }),
  });
  if (!r.ok) throw new Error(`update queue ${r.status}`);
};