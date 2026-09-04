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
    } catch { /* ignore */ }
    throw new Error(msg);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

export type SmtpConfig = {
  host: string;
  port: string;
  user: string;
  from: string;
  passSet: boolean;
  configured: boolean;
};

export const getSmtp = () => req<SmtpConfig>("/api/billing/smtp");

export const saveSmtp = (body: {
  host: string;
  port: string;
  user: string;
  pass?: string;
  from: string;
}) => req<SmtpConfig>("/api/billing/smtp", { method: "PUT", body: JSON.stringify(body) });

export const testSmtp = (to: string) =>
  req<{ ok?: boolean }>("/api/billing/smtp/test", { method: "POST", body: JSON.stringify({ to }) });
