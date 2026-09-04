import type { FormEvent } from "react";
import { useMemo, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import { CheckCircle2, Loader2, Lock, ShieldAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { resetPassword } from "@/services/auth";

export const ResetPasswordPage = () => {
  const { t } = useTranslation();
  const [params] = useSearchParams();
  const token = useMemo(() => params.get("token")?.trim() ?? "", [params]);
  const navigate = useNavigate();
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [done, setDone] = useState(false);

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (password.length < 8) {
      toast.error(t("pages.resetPassword.errMinLength", { defaultValue: "A senha deve ter pelo menos 8 caracteres." }));
      return;
    }
    if (password !== confirm) {
      toast.error(t("pages.resetPassword.errMismatch", { defaultValue: "As senhas não conferem." }));
      return;
    }
    setSubmitting(true);
    try {
      await resetPassword(token, password);
      setDone(true);
      toast.success(t("pages.resetPassword.successToast", { defaultValue: "Senha redefinida com sucesso!" }), {
        description: t("pages.resetPassword.successToastDesc", { defaultValue: "Faça login com sua nova senha." }),
      });
      setTimeout(() => navigate("/login", { replace: true }), 2500);
    } catch (err) {
      toast.error(t("pages.resetPassword.errFailed", { defaultValue: "Não foi possível redefinir" }), { description: (err as Error).message });
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="grid min-h-dvh place-items-center bg-background px-4">
      <div className="w-full max-w-md rounded-2xl border bg-card/80 p-8 shadow-xl backdrop-blur-xl">
        {!token ? (
          <div className="space-y-4 text-center">
            <ShieldAlert className="mx-auto h-10 w-10 text-destructive" />
            <h1 className="text-xl font-semibold">{t("pages.resetPassword.invalidTitle", { defaultValue: "Link inválido" })}</h1>
            <p className="text-sm text-muted-foreground">
              {t("pages.resetPassword.invalidDesc", { defaultValue: "O link de recuperação está incompleto ou expirou. Solicite um novo na tela de login." })}
            </p>
            <Button asChild className="w-full">
              <Link to="/login">{t("pages.resetPassword.backToLogin", { defaultValue: "Voltar ao login" })}</Link>
            </Button>
          </div>
        ) : done ? (
          <div className="space-y-4 text-center">
            <CheckCircle2 className="mx-auto h-10 w-10 text-emerald-500" />
            <h1 className="text-xl font-semibold">{t("pages.resetPassword.doneTitle", { defaultValue: "Senha redefinida" })}</h1>
            <p className="text-sm text-muted-foreground">
              {t("pages.resetPassword.doneDesc", { defaultValue: "Sua senha foi alterada e as sessões antigas foram encerradas. Redirecionando para o login..." })}
            </p>
            <Button asChild className="w-full">
              <Link to="/login">{t("pages.resetPassword.goToLogin", { defaultValue: "Ir para o login" })}</Link>
            </Button>
          </div>
        ) : (
          <>
            <div className="mb-6 text-center">
              <h1 className="text-2xl font-semibold tracking-tight">{t("pages.resetPassword.title", { defaultValue: "Definir nova senha" })}</h1>
              <p className="mt-1 text-sm text-muted-foreground">
                {t("pages.resetPassword.subtitle", { defaultValue: "Escolha uma senha com pelo menos 8 caracteres." })}
              </p>
            </div>
            <form onSubmit={onSubmit} className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="new-password">{t("pages.resetPassword.newPassword", { defaultValue: "Nova senha" })}</Label>
                <div className="relative">
                  <Lock className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    id="new-password"
                    type="password"
                    autoComplete="new-password"
                    required
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    className="h-11 pl-9"
                    placeholder="••••••••"
                  />
                </div>
              </div>
              <div className="space-y-2">
                <Label htmlFor="confirm-password">{t("pages.resetPassword.confirmPassword", { defaultValue: "Confirmar nova senha" })}</Label>
                <div className="relative">
                  <Lock className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    id="confirm-password"
                    type="password"
                    autoComplete="new-password"
                    required
                    value={confirm}
                    onChange={(e) => setConfirm(e.target.value)}
                    className="h-11 pl-9"
                    placeholder="••••••••"
                  />
                </div>
              </div>
              <Button type="submit" className="h-11 w-full" disabled={submitting}>
                {submitting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                {t("pages.resetPassword.submit", { defaultValue: "Redefinir senha" })}
              </Button>
            </form>
            <p className="mt-6 text-center text-xs text-muted-foreground">
              <Link to="/login" className="text-primary hover:underline">
                {t("pages.resetPassword.backToLogin", { defaultValue: "Voltar ao login" })}
              </Link>
            </p>
          </>
        )}
      </div>
    </div>
  );
};

export default ResetPasswordPage;
