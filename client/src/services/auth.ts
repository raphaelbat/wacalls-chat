import type { AuthUser, MeResponse, SignupPayload } from "@/types/auth";
import { apiUrl } from "@/lib/api-base";

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(apiUrl(path), {
    credentials: "include",
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
    ...init,
  });
  if (!res.ok) {
    let msg = `${res.status}`;
    try {
      const j = await res.json();
      if (j?.error) msg = j.error;
    } catch {
      /* ignore */
    }
    throw new Error(msg);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

export const me = () => req<MeResponse>("/api/auth/me");

export const login = (email: string, password: string) =>
  req<{ user: AuthUser }>("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password }),
  }).then((r) => r.user);

export type SignupResult =
  | { user: AuthUser; needsVerification?: false }
  | { needsVerification: true; email: string; devCode?: string };

export const signup = (payload: SignupPayload) =>
  req<SignupResult>("/api/auth/signup", {
    method: "POST",
    body: JSON.stringify(payload),
  });

export const verifyEmail = (email: string, code: string) =>
  req<{ user: AuthUser }>("/api/auth/verify-email", {
    method: "POST",
    body: JSON.stringify({ email, code }),
  }).then((r) => r.user);

export const resendActivationCode = (email: string) =>
  req<{ ok: boolean; devCode?: string }>("/api/auth/resend-code", {
    method: "POST",
    body: JSON.stringify({ email }),
  });

export const logout = () => req<void>("/api/auth/logout", { method: "POST" });

export const forgotPassword = (email: string) =>
  req<{ ok: boolean; message: string; recoveryUrl?: string }>("/api/auth/forgot-password", {
    method: "POST",
    body: JSON.stringify({ email }),
  });

export const resetPassword = (token: string, newPassword: string) =>
  req<{ ok: boolean }>("/api/auth/reset-password", {
    method: "POST",
    body: JSON.stringify({ token, newPassword }),
  });

export const listUsers = () =>
  req<{ users: AuthUser[] }>("/api/users").then((r) => r.users ?? []);

export const setRole = (id: string, role: "admin" | "user", grant: boolean) =>
  req<{ ok: string }>(`/api/users/${id}/roles`, {
    method: "POST",
    body: JSON.stringify({ role, grant }),
  });

export const deleteUser = (id: string) =>
  req<void>(`/api/users/${id}`, { method: "DELETE" });

export const createUser = (payload: {
  email: string;
  password: string;
  name?: string;
  companyName: string;
  cpf: string;
  role?: "admin" | "user";
  queueIds?: string[];
}) =>
  req<{ user: AuthUser }>("/api/users", {
    method: "POST",
    body: JSON.stringify(payload),
  }).then((r) => r.user);

export const updateUser = (
  id: string,
  payload: { email?: string; name?: string; companyName?: string; cpf?: string; newPassword?: string },
) =>
  req<void>(`/api/users/${id}`, {
    method: "PUT",
    body: JSON.stringify(payload),
  });

export const getUserQueues = (id: string) =>
  req<{ queueIds: string[] }>(`/api/users/${id}/queues`).then((r) => r.queueIds ?? []);

export const setUserQueues = (id: string, queueIds: string[]) =>
  req<void>(`/api/users/${id}/queues`, {
    method: "PUT",
    body: JSON.stringify({ queueIds }),
  });

export const getUserSessions = (id: string) =>
  req<{ sessionIds: string[] }>(`/api/users/${id}/sessions`).then((r) => r.sessionIds ?? []);

export const setUserSessions = (id: string, sessionIds: string[]) =>
  req<void>(`/api/users/${id}/sessions`, {
    method: "PUT",
    body: JSON.stringify({ sessionIds }),
  });

export const getUserPermissions = (id: string) =>
  req<{ permissions: string[] }>(`/api/users/${id}/permissions`).then((r) => r.permissions ?? []);

export const setUserPermissions = (id: string, permissions: string[]) =>
  req<void>(`/api/users/${id}/permissions`, {
    method: "PUT",
    body: JSON.stringify({ permissions }),
  });

export const listCompanies = () =>
  req<{ users: AuthUser[] }>("/api/companies").then((r) => r.users ?? []);

export const setCompanyActive = (id: string, active: boolean) =>
  req<{ ok: string }>(`/api/companies/${id}/active`, {
    method: "POST",
    body: JSON.stringify({ active }),
  });

export const deleteCompany = (id: string) =>
  req<void>(`/api/companies/${id}`, { method: "DELETE" });

export interface CompanySubscription {
  userId: string;
  planId: string;
  status: string;
  currentPeriodEnd: number;
  complimentary: boolean;
}

export const getCompanySubscription = (id: string) =>
  req<CompanySubscription>(`/api/companies/${id}/subscription`);

export const setCompanySubscription = (
  id: string,
  payload: { planId?: string; complimentary: boolean },
) =>
  req<CompanySubscription>(`/api/companies/${id}/subscription`, {
    method: "PUT",
    body: JSON.stringify(payload),
  });


export const updateEmail = (email: string, password: string) =>
  req<{ email: string }>("/api/me/email", {
    method: "PUT",
    body: JSON.stringify({ email, password }),
  });

export const updatePassword = (currentPassword: string, newPassword: string) =>
  req<{ ok: string }>("/api/me/password", {
    method: "PUT",
    body: JSON.stringify({ currentPassword, newPassword }),
  });

export const uploadAvatar = async (file: File): Promise<{ avatarUrl: string }> => {
  const fd = new FormData();
  fd.append("file", file);
  const res = await fetch(apiUrl("/api/me/avatar"), {
    method: "POST",
    credentials: "include",
    body: fd,
  });
  if (!res.ok) {
    let msg = `${res.status}`;
    try {
      const j = await res.json();
      if (j?.error) msg = j.error;
    } catch {
      /* ignore */
    }
    throw new Error(msg);
  }
  return res.json();
};

export const deleteAvatar = () =>
  req<void>("/api/me/avatar", { method: "DELETE" });

export type RoleAuditEntry = {
  id: string;
  targetId: string;
  targetEmail: string;
  actorId: string;
  actorEmail: string;
  role: string;
  granted: boolean;
  prevGranted: boolean;
  createdAt: number;
};

export const listRoleAudit = (limit = 50) =>
  req<{ entries: RoleAuditEntry[] }>(`/api/audit/roles?limit=${limit}`).then((r) => r.entries ?? []);
