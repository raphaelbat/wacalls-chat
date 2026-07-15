import { getClientId } from "./client-id";
import { apiUrl } from "./api-base";
import { handleQuotaResponse, type QuotaPayload } from "./quota";

const baseHeaders = (): HeadersInit => ({
  "X-Client-Id": getClientId(),
  "Content-Type": "application/json",
});

export class ApiError extends Error {
  status: number;
  payload: unknown;
  /** true quando o erro já foi tratado por um handler global (ex: 402 quota). */
  handled: boolean;
  constructor(message: string, status: number, payload: unknown, handled = false) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.payload = payload;
    this.handled = handled;
  }
}

async function parseError(path: string, r: Response): Promise<ApiError> {
  let payload: unknown = null;
  let text = "";
  try {
    text = await r.text();
    payload = text ? JSON.parse(text) : null;
  } catch {
    payload = text || null;
  }
  // Sessão revogada (login em outro navegador / logout remoto).
  // Emite um evento global para o app reagir (limpar estado + redirecionar).
  if (r.status === 401 && !path.includes("/api/auth/")) {
    try {
      window.dispatchEvent(new CustomEvent("auth:invalidated"));
    } catch {
      /* noop */
    }
  }
  // Tratamento unificado de 402 Payment Required (cotas de plano).
  if (r.status === 402 && payload && typeof payload === "object") {
    const handled = handleQuotaResponse(payload as QuotaPayload);
    if (handled) {
      const msg = (payload as { error?: string }).error || `${path} 402`;
      return new ApiError(msg, 402, payload, true);
    }
  }
  let msg = `${path} ${r.status}`;
  if (payload && typeof payload === "object") {
    const e = (payload as { error?: unknown }).error;
    if (typeof e === "string" && e) msg = e;
  } else if (typeof payload === "string" && payload) {
    msg = payload;
  }
  return new ApiError(msg, r.status, payload, false);
}

export const apiGet = async <T>(path: string): Promise<T> => {
  const r = await fetch(apiUrl(path), { headers: baseHeaders(), credentials: "include" });
  if (!r.ok) throw await parseError(path, r);
  return r.json() as Promise<T>;
};

export const apiPost = async <T>(path: string, body: unknown): Promise<T> => {
  const r = await fetch(apiUrl(path), {
    method: "POST",
    headers: baseHeaders(),
    credentials: "include",
    body: JSON.stringify(body),
  });
  if (!r.ok) throw await parseError(path, r);
  return r.json() as Promise<T>;
};

export const apiDelete = async (path: string): Promise<void> => {
  const r = await fetch(apiUrl(path), { method: "DELETE", headers: baseHeaders(), credentials: "include" });
  if (!r.ok) throw await parseError(path, r);
};

export const apiPatch = async <T>(path: string, body: unknown): Promise<T> => {
  const r = await fetch(apiUrl(path), {
    method: "PATCH",
    headers: baseHeaders(),
    credentials: "include",
    body: JSON.stringify(body),
  });
  if (!r.ok) throw await parseError(path, r);
  return r.json() as Promise<T>;
};
