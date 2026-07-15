import { useState } from "react";
import { Loader2, Mail } from "lucide-react";
import { toast } from "sonner";
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
      toast.error("Informe o e-mail cadastrado.");
      return;
    }
    setSubmitting(true);
    try {
      const r = await forgotPassword(email.trim());
      setSentMessage(r.message);
      setRecoveryUrl(r.recoveryUrl ?? null);
      if (!r.recoveryUrl) {
        toast.success("Verifique seu e-mail para o link de recuperação.");
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
          <DialogTitle>Recuperar senha</DialogTitle>
          <DialogDescription>
            Informe o e-mail da sua conta. Enviaremos um link para definir uma nova senha
            (válido por 1 hora).
          </DialogDescription>
        </DialogHeader>

        {sentMessage ? (
          <div className="space-y-3 text-sm">
            <p>{sentMessage}</p>
            {recoveryUrl && (
              <div className="space-y-2 rounded-md border bg-muted/40 p-3">
                <p className="text-xs text-muted-foreground">
                  Link de recuperação (copie e abra em outro dispositivo, se necessário):
                </p>
                <Input readOnly value={recoveryUrl} onFocus={(e) => e.currentTarget.select()} />
                <div className="flex justify-end">
                  <Button
                    type="button"
                    size="sm"
                    variant="secondary"
                    onClick={() => {
                      navigator.clipboard.writeText(recoveryUrl).then(
                        () => toast.success("Link copiado."),
                        () => toast.error("Não foi possível copiar."),
                      );
                    }}
                  >
                    Copiar link
                  </Button>
                </div>
              </div>
            )}
            <div className="flex justify-end gap-2 pt-2">
              <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                Fechar
              </Button>
            </div>
          </div>
        ) : (
          <form onSubmit={onSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="forgot-email">E-mail</Label>
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
                Cancelar
              </Button>
              <Button type="submit" disabled={submitting}>
                {submitting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                Enviar link
              </Button>
            </div>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
};