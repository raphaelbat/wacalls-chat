import { useEffect, useState } from "react";
import { toast } from "sonner";
import { Cloud, Copy, Power, PowerOff, Save } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { apiGet, apiPost } from "@/lib/api";
import type { SessionInfo } from "@/types/session";

type CloudStatus = {
  mode?: string;
  phoneId?: string;
  wabaId?: string;
  webhookUrl?: string;
  verifyToken?: string;
  hasToken?: boolean;
  hasAppSecret?: boolean;
};

type EnableResp = CloudStatus & { ok?: boolean };

type EnablePayload = {
  phoneId: string;
  wabaId: string;
  token: string;
  appSecret?: string;
};

const copy = (v?: string) => {
  if (!v) return;
  navigator.clipboard.writeText(v).then(
    () => toast.success("Copiado"),
    () => toast.error("Falha ao copiar"),
  );
};

export const CloudApiTab = ({ session }: { session: SessionInfo }) => {
  const sid = session.id;
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [disabling, setDisabling] = useState(false);
  const [status, setStatus] = useState<CloudStatus>({});
  const enabled = status.mode === "cloud";
  const [form, setForm] = useState<EnablePayload>({
    phoneId: "",
    wabaId: "",
    token: "",
    appSecret: "",
  });

  const refresh = async () => {
    setLoading(true);
    try {
      const s = await apiGet<CloudStatus>(`/api/sessions/${sid}/cloud`);
      setStatus(s || {});
      setForm((f) => ({
        ...f,
        phoneId: s?.phoneId ?? f.phoneId,
        wabaId: s?.wabaId ?? f.wabaId,
      }));
    } catch {
      setStatus({});
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sid]);

  const enable = async () => {
    if (!form.phoneId.trim() || !form.wabaId.trim() || !form.token.trim()) {
      toast.error("Preencha Phone ID, WABA ID e Token");
      return;
    }
    setSaving(true);
    try {
      const r = await apiPost<EnableResp>(`/api/sessions/${sid}/cloud/enable`, {
        phoneId: form.phoneId.trim(),
        wabaId: form.wabaId.trim(),
        token: form.token.trim(),
        appSecret: form.appSecret?.trim() || undefined,
      });
      setForm((f) => ({ ...f, token: "", appSecret: "" }));
      toast.success("API Oficial conectada");
      await refresh();
      // preserve webhook/verify token returned by enable in case refresh omits it
      setStatus((prev) => ({
        ...prev,
        webhookUrl: r.webhookUrl ?? prev.webhookUrl,
        verifyToken: r.verifyToken ?? prev.verifyToken,
      }));
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setSaving(false);
    }
  };

  const disable = async () => {
    if (!confirm("Desativar a conexão com a API Oficial desta sessão?")) return;
    setDisabling(true);
    try {
      await apiPost(`/api/sessions/${sid}/cloud/disable`, {});
      setStatus({});
      toast.success("API Oficial desconectada");
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setDisabling(false);
    }
  };

  return (
    <div className="space-y-5">
      <section className="rounded-xl border bg-card p-5">
        <header className="mb-4 flex items-center gap-2">
          <Cloud className="h-4 w-4 text-primary" />
          <h2 className="text-sm font-semibold">API Oficial (Meta Cloud API)</h2>
          <span
            className={`ml-auto inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-[11px] font-semibold ${
              enabled
                ? "border border-emerald-500/40 bg-emerald-500/10 text-emerald-400"
                : "bg-muted text-muted-foreground"
            }`}
          >
            {enabled ? "Ativa" : "Não configurada"}
          </span>
        </header>

        <p className="mb-4 text-xs text-muted-foreground">
          Conecte esta sessão à WhatsApp Cloud API oficial da Meta. As credenciais são
          armazenadas criptografadas e usadas para envio de mensagens, templates e recebimento
          via webhook.
        </p>

        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <Label className="text-xs font-semibold">Phone Number ID</Label>
            <Input
              value={form.phoneId}
              onChange={(e) => setForm((f) => ({ ...f, phoneId: e.target.value }))}
              placeholder="ex: 123456789012345"
              maxLength={64}
              disabled={loading || saving}
              className="mt-1.5"
            />
          </div>
          <div>
            <Label className="text-xs font-semibold">WABA ID</Label>
            <Input
              value={form.wabaId}
              onChange={(e) => setForm((f) => ({ ...f, wabaId: e.target.value }))}
              placeholder="ex: 987654321098765"
              maxLength={64}
              disabled={loading || saving}
              className="mt-1.5"
            />
          </div>
          <div>
            <Label className="text-xs font-semibold">Access Token</Label>
            <Input
              type="password"
              value={form.token}
              onChange={(e) => setForm((f) => ({ ...f, token: e.target.value }))}
              placeholder={enabled ? "•••••• (deixe em branco para manter)" : "EAAG..."}
              maxLength={2048}
              disabled={loading || saving}
              className="mt-1.5"
              autoComplete="new-password"
            />
          </div>
          <div>
            <Label className="text-xs font-semibold">App Secret (opcional)</Label>
            <Input
              type="password"
              value={form.appSecret ?? ""}
              onChange={(e) => setForm((f) => ({ ...f, appSecret: e.target.value }))}
              placeholder="para validar X-Hub-Signature-256"
              maxLength={512}
              disabled={loading || saving}
              className="mt-1.5"
              autoComplete="new-password"
            />
          </div>
        </div>

        <div className="mt-5 flex flex-wrap justify-end gap-2">
          {enabled && (
            <Button
              variant="outline"
              onClick={() => void disable()}
              disabled={disabling || saving}
            >
              <PowerOff className="h-4 w-4" /> Desativar
            </Button>
          )}
          <Button onClick={() => void enable()} disabled={saving || loading}>
            {enabled ? (
              <>
                <Save className="h-4 w-4" /> Atualizar credenciais
              </>
            ) : (
              <>
                <Power className="h-4 w-4" /> Conectar
              </>
            )}
          </Button>
        </div>
      </section>

      {enabled && (status.webhookUrl || status.verifyToken) && (
        <section className="rounded-xl border bg-card p-5">
          <header className="mb-3">
            <h3 className="text-sm font-semibold">Webhook para o painel Meta</h3>
            <p className="mt-1 text-xs text-muted-foreground">
              Cole estes valores em <strong>Meta for Developers → WhatsApp → Configuration → Webhook</strong>
              . Assine os eventos <code>messages</code>, <code>message_template_status_update</code> e{" "}
              <code>account_update</code>.
            </p>
          </header>
          <div className="space-y-3">
            {status.webhookUrl && (
              <ReadOnlyField label="Callback URL" value={status.webhookUrl} />
            )}
            {status.verifyToken && (
              <ReadOnlyField label="Verify Token" value={status.verifyToken} />
            )}
          </div>
        </section>
      )}
    </div>
  );
};

const ReadOnlyField = ({ label, value }: { label: string; value: string }) => (
  <div>
    <Label className="text-xs font-semibold">{label}</Label>
    <div className="mt-1.5 flex gap-2">
      <Input readOnly value={value} className="font-mono text-xs" />
      <Button variant="outline" type="button" onClick={() => copy(value)}>
        <Copy className="h-4 w-4" />
      </Button>
    </div>
  </div>
);
