import { useState } from "react";
import { Loader2, Mail } from "lucide-react";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { forgotPassword } from "@/services/auth";

type Props = {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  initialEmail?: string;
};

export const ForgotPasswordDialog = ({ open, onOpenChange, initialEmail = "" }: Props) => {
  const { t } = useTranslation();
  const [email, setEmail] = useState(initialEmail);
  const [submitting, setSubmitting] = useState(false);
  const [recoveryUrl, setRecoveryUrl] = useState<string | null>(null);
  const [sentMessage, setSentMessage] = useState<string | null>(null);

  const reset = () => {
    setRecoveryUrl(null);
    setSentMessage(null);
  };

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!email.trim()) {
      toast.error(t("pages.forgotPassword.errEmail", { defaultValue: "Informe o e-mail cadastrado." }));
      return;
    }
    setSubmitting(true);
    try {
      const r = await forgotPassword(email.trim());
      setSentMessage(r.message);
      setRecoveryUrl(r.recoveryUrl ?? null);
      if (!r.recoveryUrl) {
        toast.success(t("pages.forgotPassword.checkEmailToast", { defaultValue: "Verifique seu e-mail para o link de recuperação." }));
      }
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!v) reset();
        onOpenChange(v);
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("pages.forgotPassword.title", { defaultValue: "Recuperar senha" })}</DialogTitle>
          <DialogDescription>
            {t("pages.forgotPassword.description", {
              defaultValue: "Informe o e-mail da sua conta. Enviaremos um link para definir uma nova senha (válido por 1 hora).",
            })}
          </DialogDescription>
        </DialogHeader>

        {sentMessage ? (
          <div className="space-y-3 text-sm">
            <p>{sentMessage}</p>
            {recoveryUrl && (
              <div className="space-y-2 rounded-md border bg-muted/40 p-3">
                <p className="text-xs text-muted-foreground">
                  {t("pages.forgotPassword.recoveryLinkHint", { defaultValue: "Link de recuperação (copie e abra em outro dispositivo, se necessário):" })}
                </p>
                <Input readOnly value={recoveryUrl} onFocus={(e) => e.currentTarget.select()} />
                <div className="flex justify-end">
                  <Button
                    type="button"
                    size="sm"
                    variant="secondary"
                    onClick={() => {
                      navigator.clipboard.writeText(recoveryUrl).then(
                        () => toast.success(t("pages.forgotPassword.copiedToast", { defaultValue: "Link copiado." })),
                        () => toast.error(t("pages.forgotPassword.copyFailedToast", { defaultValue: "Não foi possível copiar." })),
                      );
                    }}
                  >
                    {t("pages.forgotPassword.copyLink", { defaultValue: "Copiar link" })}
                  </Button>
                </div>
              </div>
            )}
            <div className="flex justify-end gap-2 pt-2">
              <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                {t("common.close", { defaultValue: "Fechar" })}
              </Button>
            </div>
          </div>
        ) : (
          <form onSubmit={onSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="forgot-email">{t("auth.email", { defaultValue: "E-mail" })}</Label>
              <div className="relative">
                <Mail className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  id="forgot-email"
                  type="email"
                  autoComplete="email"
                  required
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="seu@email.com"
                  className="pl-10"
                />
              </div>
            </div>
            <div className="flex justify-end gap-2 pt-2">
              <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                {t("common.cancel", { defaultValue: "Cancelar" })}
              </Button>
              <Button type="submit" disabled={submitting}>
                {submitting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                {t("pages.forgotPassword.sendLink", { defaultValue: "Enviar link" })}
              </Button>
            </div>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
};
