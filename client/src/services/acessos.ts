import { apiUrl } from "@/lib/api-base";

/**
 * Acessos abertos: quem está logado, de onde, desde quando.
 * Atendente enxerga os próprios; admin enxerga os de todo mundo.
 */
export interface Acesso {
  /** Só o início do token — o valor inteiro nunca sai do servidor. */
  token: string;
  userId: string;
  email: string;
  nome: string;
  /** User-Agent cru; a tela traduz para "Chrome no Windows". */
  navegador: string;
  ip: string;
  desde: number;
  visto: number;
  /** true quando é o navegador que está fazendo a consulta. */
  atual: boolean;
}

const pedir = async <T>(path: string, init?: RequestInit): Promise<T> => {
  const res = await fetch(apiUrl(path), { credentials: "include", ...init });
  if (!res.ok) {
    const txt = await res.text().catch(() => "");
    let msg = txt || String(res.status);
    try {
      const j = JSON.parse(txt) as { error?: string };
      if (j?.error) msg = j.error;
    } catch {
      /* corpo não era JSON */
    }
    throw new Error(msg);
  }
  return (res.status === 204 ? undefined : await res.json()) as T;
};

export const listarAcessos = () =>
  pedir<{ acessos: Acesso[] }>("/api/auth/acessos").then((r) => r.acessos ?? []);

export const encerrarAcesso = (token: string) =>
  pedir<void>(`/api/auth/acessos/${encodeURIComponent(token)}`, { method: "DELETE" });
