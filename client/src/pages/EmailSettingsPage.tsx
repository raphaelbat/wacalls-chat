import { useEffect, useState } from "react";
import { Loader2, Mail, Save, Send } from "lucide-react";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import * as smtpApi from "@/services/smtp";

export const EmailSettingsPage = () => {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [passSet, setPassSet] = useState(false);
  const [configured, setConfigured] = useState(false);
  const [form, setForm] = useState({ host: "", port: "587", user: "", pass: "", from: "" });
  const [testTo, setTestTo] = useState("");

  useEffect(() => {
    let alive = true;
    smtpApi
      .getSmtp()
      .then((c) => {
        if (!alive) return;
        setForm({ host: c.host || "", port: c.port || "587", user: c.user || "", pass: "", from: c.from || "" });
        setPassSet(!!c.passSet);
        setConfigured(!!c.configured);
      })
      .catch((e) => toast.error(e instanceof Error ? e.message : t("pages.emailSettings.errLoad", { defaultValue: "Falha ao carregar SMTP" })))
      .finally(() => alive && setLoading(false));
    return () => { alive = false; };
  }, [t]);

  const set = (k: keyof typeof form) => (e: { target: { value: string } }) =>
    setForm((f) => ({ ...f, [k]: e.target.value }));

  const save = async () => {
    if (!form.host.trim() || !form.from.trim()) {
      toast.error(t("pages.emailSettings.errRequired", { defaultValue: "Informe ao menos servidor (host) e remetente (from)." }));
      return;
    }
    setSaving(true);
    try {
      const c = await smtpApi.saveSmtp({
        host: form.host.trim(),
        port: form.port.trim() || "587",
        user: form.user.trim(),
        pass: form.pass.trim() || undefined,
        from: form.from.trim(),
      });
      setPassSet(!!c.passSet);
      setConfigured(!!c.configured);
      setForm((f) => ({ ...f, pass: "" }));
      toast.success(t("pages.emailSettings.savedToast", { defaultValue: "Configurações de e-mail salvas." }));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("pages.emailSettings.errSave", { defaultValue: "Falha ao salvar" }));
    } finally {
      setSaving(false);
    }
  };

  const sendTest = async () => {
    if (!testTo.trim()) {
      toast.error(t("pages.emailSettings.errTestTo", { defaultValue: "Informe um e-mail de destino para o teste." }));
      return;
    }
    setTesting(true);
    try {
      await smtpApi.testSmtp(testTo.trim());
      toast.success(t("pages.emailSettings.testSentToast", { defaultValue: "E-mail de teste enviado." }));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("pages.emailSettings.errTestSend", { defaultValue: "Falha no envio de teste" }));
    } finally {
      setTesting(false);
    }
  };

  return (
    <AppShell>
      <div className="mx-auto w-full max-w-2xl p-4 sm:p-6">
        <header className="mb-6 flex items-center gap-3">
          <span className="grid h-10 w-10 place-items-center rounded-xl bg-primary/10 text-primary">
            <Mail className="h-5 w-5" />
          </span>
          <div>
            <h1 className="text-lg font-semibold">{t("pages.emailSettings.title", { defaultValue: "E-mail / SMTP" })}</h1>
            <p className="text-sm text-muted-foreground">
              {t("pages.emailSettings.subtitle", { defaultValue: "Servidor usado para recuperação de senha e notificações." })}
            </p>
          </div>
          <span
            className={`ml-auto rounded-full px-2.5 py-1 text-xs font-medium ${
              configured ? "bg-emerald-500/15 text-emerald-500" : "bg-muted text-muted-foreground"
            }`}
          >
            {configured
              ? t("pages.emailSettings.configured", { defaultValue: "Configurado" })
              : t("pages.emailSettings.notConfigured", { defaultValue: "Não configurado" })}
          </span>
        </header>

        {loading ? (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" /> {t("common.loading", { defaultValue: "Carregando…" })}
          </div>
        ) : (
          <div className="space-y-5 rounded-xl border bg-card p-4 sm:p-5">
            <div className="grid gap-4 sm:grid-cols-3">
              <div className="sm:col-span-2 space-y-1.5">
                <Label htmlFor="smtp-host">{t("pages.emailSettings.hostLabel", { defaultValue: "Servidor (host)" })}</Label>
                <Input id="smtp-host" placeholder="smtp.gmail.com" value={form.host} onChange={set("host")} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="smtp-port">{t("pages.emailSettings.portLabel", { defaultValue: "Porta" })}</Label>
                <Input id="smtp-port" placeholder="587" value={form.port} onChange={set("port")} />
              </div>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="smtp-user">{t("pages.emailSettings.userLabel", { defaultValue: "Usuário" })}</Label>
              <Input id="smtp-user" autoComplete="off" placeholder="usuario@dominio.com" value={form.user} onChange={set("user")} />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="smtp-pass">{t("pages.emailSettings.passLabel", { defaultValue: "Senha" })}</Label>
              <Input
                id="smtp-pass"
                type="password"
                autoComplete="new-password"
                placeholder={passSet ? t("pages.emailSettings.passKeptPlaceholder", { defaultValue: "•••••••• (mantida)" }) : t("pages.emailSettings.passPlaceholder", { defaultValue: "senha ou token do app" })}
                value={form.pass}
                onChange={set("pass")}
              />
              <p className="text-xs text-muted-foreground">
                {passSet
                  ? t("pages.emailSettings.passKeptHint", { defaultValue: "Deixe em branco para manter a senha atual." })
                  : t("pages.emailSettings.passRequiredHint", { defaultValue: "Necessária quando o servidor exige autenticação." })}
              </p>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="smtp-from">{t("pages.emailSettings.fromLabel", { defaultValue: "Remetente (from)" })}</Label>
              <Input id="smtp-from" placeholder="nao-responda@seudominio.com" value={form.from} onChange={set("from")} />
            </div>

            <div className="flex justify-end">
              <Button onClick={save} disabled={saving}>
                {saving ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Save className="mr-2 h-4 w-4" />}
                {t("common.save")}
              </Button>
            </div>

            <div className="border-t pt-4">
              <Label htmlFor="smtp-test">{t("pages.emailSettings.testLabel", { defaultValue: "Enviar e-mail de teste" })}</Label>
              <div className="mt-1.5 flex flex-col gap-2 sm:flex-row">
                <Input
                  id="smtp-test"
                  placeholder="destinatario@dominio.com"
                  value={testTo}
                  onChange={(e) => setTestTo(e.target.value)}
                />
                <Button variant="outline" onClick={sendTest} disabled={testing}>
                  {testing ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Send className="mr-2 h-4 w-4" />}
                  {t("pages.emailSettings.testButton", { defaultValue: "Testar" })}
                </Button>
              </div>
            </div>
          </div>
        )}
      </div>
    </AppShell>
  );
};

export default EmailSettingsPage;
