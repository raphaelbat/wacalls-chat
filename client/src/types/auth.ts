export type AuthUser = {
  id: string;
  email: string;
  name?: string;
  roles: string[];
  companyName?: string;
  cpf?: string;
  active?: boolean;
  createdAt?: number;
  avatarUrl?: string;
  signatureEnabled?: boolean;
  signature?: string;
  permissions?: string[];
  parentId?: string;
};

export type MeResponse = {
  user: AuthUser | null;
  needsSignup?: boolean;
};

export type SignupPayload = {
  email: string;
  password: string;
  companyName: string;
  cpf: string;
};