import { apiUrl } from "@/lib/api-base";

/**
 * Estado da licença desta instalação. Serve só para a faixa de aviso: quem
 * paga em dia nunca vê nada, porque a renovação acontece sozinha em segundo
 * plano. A faixa existe para o caso em que a cobrança falhou.
 */
export interface LicenseStatus {
  /** false na versão livre — aí não há nada para avisar. */
  exigida: boolean;
  valida: boolean;
  vitalicia: boolean;
  codigo?: string;
  plano?: string;
  /** epoch em segundos; 0 = sem validade */
  expiraEm?: number;
  /** negativo quando já venceu */
  diasParaVencer: number;
  /** vencida, mas ainda dentro dos dias de tolerância */
  emTolerancia: boolean;
  diasTolerancia?: number;
  motivo?: string;
  suporte: string;
}

export const getLicenseStatus = async (): Promise<LicenseStatus> => {
  const res = await fetch(apiUrl("/api/license"), { credentials: "include" });
  if (!res.ok) throw new Error(String(res.status));
  return (await res.json()) as LicenseStatus;
};
