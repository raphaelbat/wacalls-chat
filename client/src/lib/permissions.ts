// Catalog of fine-grained capabilities an admin can grant to a user.
// Admins implicitly have all permissions regardless of this list.
export type Permission =
  | "dialer"
  | "chats"
  | "contacts"
  | "connections"
  | "flows"
  | "queues"
  | "tags"
  | "kanban"
  | "campaigns"
  | "reports"
  | "api"
  | "agents";

export type PermissionDef = {
  key: Permission;
  label: string;
  description: string;
};

export const PERMISSIONS: PermissionDef[] = [
  { key: "dialer", label: "Discador", description: "Realizar ligações pelo discador." },
  { key: "chats", label: "Conversas", description: "Atender chats e ver mensagens." },
  { key: "contacts", label: "Contatos", description: "Visualizar e gerenciar contatos." },
  { key: "connections", label: "Conexões", description: "Parear e administrar conexões WhatsApp." },
  { key: "flows", label: "Fluxos", description: "Criar e editar fluxos do FlowBuilder." },
  { key: "queues", label: "Filas", description: "Gerenciar filas de atendimento." },
  { key: "tags", label: "Tags", description: "Criar e organizar tags." },
  { key: "kanban", label: "Kanban", description: "Acessar o quadro Kanban." },
  { key: "campaigns", label: "Campanhas", description: "Criar e disparar campanhas." },
  { key: "reports", label: "Relatórios", description: "Visualizar relatórios e BI." },
  { key: "api", label: "API", description: "Gerar tokens e usar a API externa." },
  { key: "agents", label: "Agentes IA", description: "Configurar agentes de voz com IA (ElevenLabs)." },
];

// Default set for newly created non-admin users.
export const ALL_PERMISSIONS: Permission[] = PERMISSIONS.map((p) => p.key);

export const DEFAULT_PERMISSIONS: Permission[] = [...ALL_PERMISSIONS];

// True if the user can access the capability (admins always pass).
export const hasPermission = (
  user: { roles?: string[]; permissions?: string[]; parentId?: string } | null | undefined,
  perm: Permission,
): boolean => {
  if (!user) return false;
  if (user.roles?.includes("admin")) return true;
  // Conta raiz de cliente/empresa não é operador interno; deve enxergar o
  // sistema contratado. Subusuários (parentId preenchido) continuam limitados
  // pelas permissões marcadas no cadastro do usuário.
  if (!user.parentId) return true;
  return !!user.permissions?.includes(perm);
};