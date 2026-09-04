import { toast } from "sonner";
import { ApiError } from "./api";

/**
 * Mostra um toast de erro respeitando handlers globais (ex.: cota 402 que já
 * emitiu seu próprio toast padronizado). Use em catch() de mutações.
 */
export function toastError(e: unknown, fallback = "Ocorreu um erro inesperado.") {
  if (e instanceof ApiError && e.handled) return; // já tratado globalmente
  const msg = e instanceof Error ? e.message : String(e ?? fallback);
  toast.error(msg || fallback);
}