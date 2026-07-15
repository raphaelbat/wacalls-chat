export type Queue = {
  id: string;
  name: string;
  color: string;
  ownerId?: string;
  createdAt: number;
  orderBot?: string;
  closeTicket?: boolean;
  rotation?: boolean;
  rotationInterval?: string;
  rotationMode?: string;
  autoRandomize?: boolean;
  agentId?: string;
  greeting?: string;
};