import { useState } from "react";
import { AlertTriangle, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";

// Modal de motivo de encerramento — usado tanto pelo botão X da lista
// de conversas quanto por outros pontos que disparem o fechamento.
// O motivo é obrigatório aqui porque o componente só é exibido quando a
// opção "MOTIVO DE ENCERRAMENTO" estiver habilitada nas Configurações.
export const CloseReasonDialog = ({
  chatName,
  onCancel,
  onConfirm,
}: {
  chatName: string;
  onCancel: () => void;
  onConfirm: (reason: string) => Promise<void> | void;
}) => {
  const { t } = useTranslation();
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    if (!reason.trim() || busy) return;
    setBusy(true);
    try {
      await onConfirm(reason.trim());
    } finally {
      setBusy(false);
    }
  };
  return (
    <div
      className="fixed inset-0 z-50 grid place-items-center bg-black/50 p-4"
      onClick={() => !busy && onCancel()}
    >
      <div
        className="w-full max-w-md rounded-lg border bg-card p-4 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-3 flex items-start gap-3">
          <span className="grid h-9 w-9 shrink-0 place-items-center rounded-full bg-amber-500/15 text-amber-500">
            <AlertTriangle className="h-4 w-4" />
          </span>
          <div className="min-w-0 flex-1">
            <div className="text-sm font-semibold">{t("chat.closeConfirmTitle", { defaultValue: "Finalizar atendimento?" })}</div>
            <div className="text-xs text-muted-foreground">
              {t("chat.closeConfirmDesc", { defaultValue: "Esta ação encerra a conversa com {{name}}. Registre o motivo para histórico.", name: chatName })}
            </div>
          </div>
          <button
            type="button"
            disabled={busy}
            onClick={onCancel}
            className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <label className="mb-1 block text-xs font-medium">{t("chat.closeReasonLabel", { defaultValue: "Motivo do encerramento" })}</label>
        <textarea
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          placeholder={t("chat.closeReasonPlaceholder", { defaultValue: "Ex.: Dúvida resolvida, cliente sem retorno, transferido para outro setor…" })}
          rows={3}
          autoFocus
          className="w-full resize-none rounded-md border bg-background px-2 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
          disabled={busy}
        />
        <div className="mt-3 flex justify-end gap-2">
          <Button size="sm" variant="ghost" disabled={busy} onClick={onCancel}>{t("common.cancel", { defaultValue: "Cancelar" })}</Button>
          <Button size="sm" variant="destructive" disabled={busy || !reason.trim()} onClick={() => void submit()}>
            {busy ? t("chat.finishing", { defaultValue: "Finalizando…" }) : t("chat.finishService", { defaultValue: "Finalizar atendimento" })}
          </Button>
        </div>
      </div>
    </div>
  );
};
