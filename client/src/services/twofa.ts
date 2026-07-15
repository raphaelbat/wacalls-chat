// 2FA storage service.
// Tries the backend endpoints (/api/auth/2fa/*) first. When the backend
// does not implement them yet, falls back to localStorage so the wizard
// remains fully functional during the transition.

import { apiUrl } from "@/lib/api-base";
import { verifyCode } from "@/lib/totp";

export type TwoFAStatus = {
  enabled: boolean;
  enrolledAt?: number;
};

export type TwoFAEnrollRequest = {
  secret: string;
  recoveryCodes: string[];
  code: string; // 6-digit confirmation
};

const LS_PREFIX = "vozzap.2fa.";
const lsKey = (userId: string) => `${LS_PREFIX}${userId}`;

type LocalRecord = {
  secret: string;
  recoveryCodes: string[];
  enrolledAt: number;
};

const readLocal = (userId: string): LocalRecord | null => {
  try {
    const raw = localStorage.getItem(lsKey(userId));
    return raw ? (JSON.parse(raw) as LocalRecord) : null;
  } catch {
    return null;
  }
};

const writeLocal = (userId: string, rec: LocalRecord | null): void => {
  try {
    if (rec) localStorage.setItem(lsKey(userId), JSON.stringify(rec));
    else localStorage.removeItem(lsKey(userId));
  } catch {
    /* ignore */
  }
};

async function tryBackend<T>(path: string, init?: RequestInit): Promise<T | null> {
  try {
    const res = await fetch(apiUrl(path), {
      credentials: "include",
      headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
      ...init,
    });
    if (res.status === 404) return null; // backend doesn't implement yet
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
    return (await res.json()) as T;
  } catch (err) {
    // Network or 404-ish: fall through to local fallback.
    if ((err as Error).message?.match(/^\d+$/)) throw err;
    return null;
  }
}

export const getStatus = async (userId: string): Promise<TwoFAStatus> => {
  const remote = await tryBackend<TwoFAStatus>("/api/auth/2fa/status");
  if (remote) return remote;
  const local = readLocal(userId);
  return { enabled: !!local, enrolledAt: local?.enrolledAt };
};

export const enroll = async (
  userId: string,
  payload: TwoFAEnrollRequest,
): Promise<{ ok: true }> => {
  const remote = await tryBackend<{ ok: true }>("/api/auth/2fa/enroll", {
    method: "POST",
    body: JSON.stringify(payload),
  });
  if (remote) return remote;
  // Local verification before saving.
  if (!verifyCode(payload.secret, payload.code)) {
    throw new Error("Código inválido");
  }
  writeLocal(userId, {
    secret: payload.secret,
    recoveryCodes: payload.recoveryCodes,
    enrolledAt: Date.now(),
  });
  return { ok: true };
};

export const disable = async (userId: string, code: string): Promise<{ ok: true }> => {
  const remote = await tryBackend<{ ok: true }>("/api/auth/2fa/disable", {
    method: "POST",
    body: JSON.stringify({ code }),
  });
  if (remote) return remote;
  const local = readLocal(userId);
  if (!local) throw new Error("2FA não está ativo");
  if (!verifyCode(local.secret, code) && !local.recoveryCodes.includes(code.trim().toUpperCase())) {
    throw new Error("Código inválido");
  }
  writeLocal(userId, null);
  return { ok: true };
};

/** Verify a code locally against a user that's already enrolled (used at login). */
export const verifyLocal = (userId: string, code: string): boolean => {
  const local = readLocal(userId);
  if (!local) return true; // not enrolled — nothing to verify
  const trimmed = code.trim();
  if (verifyCode(local.secret, trimmed)) return true;
  const upper = trimmed.toUpperCase();
  const idx = local.recoveryCodes.indexOf(upper);
  if (idx >= 0) {
    // Consume recovery code
    const next = [...local.recoveryCodes];
    next.splice(idx, 1);
    writeLocal(userId, { ...local, recoveryCodes: next });
    return true;
  }
  return false;
};

export const isLocallyEnrolled = (userId: string): boolean => !!readLocal(userId);
