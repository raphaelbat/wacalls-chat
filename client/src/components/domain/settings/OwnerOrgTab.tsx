import { useEffect, useState } from "react";
import { Building2, Loader2, Save } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import * as authApi from "@/services/auth";
import { useAuth } from "@/stores/auth";

// Shows only the account owner's organization data (name + CPF/CNPJ),
// editable inline. Multi-company management lives elsewhere — this tab
// is intentionally scoped to the signed-in user's own organization.
export const OwnerOrgTab = () => {
  const user = useAuth((s) => s.user);
  const refresh = useAuth((s) => s.refresh);
  const [name, setName] = useState(user?.companyName ?? "");
  const [doc, setDoc] = useState(user?.cpf ?? "");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setName(user?.companyName ?? "");
    setDoc(user?.cpf ?? "");
  }, [user?.companyName, user?.cpf]);

  const formatDoc = (raw: string) => {
    const d = raw.replace(/\D/g, "").slice(0, 14);
    if (d.length <= 11) {
      return d
        .replace(/(\d{3})(\d)/, "$1.$2")
        .replace(/(\d{3})\.(\d{3})(\d)/, "$1.$2.$3")
        .replace(/\.(\d{3})(\d)/, ".$1-$2");
    }
    return d
      .replace(/^(\d{2})(\d)/, "$1.$2")
      .replace(/^(\d{2})\.(\d{3})(\d)/, "$1.$2.$3")
      .replace(/\.(\d{3})(\d)/, ".$1/$2")
      .replace(/(\d{4})(\d)/, "$1-$2");
  };

  const save = async () => {
    if (!user) return;
    setSaving(true);
    try {
      await authApi.updateUser(user.id, {
        companyName: name.trim(),
        cpf: doc.replace(/\D/g, ""),
      });
      toast.success("Dados da organização atualizados");
      await refresh?.();
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-6">
      <header>
        <h2 className="flex items-center gap-2 text-lg font-semibold">
          <Building2 className="h-5 w-5 text-primary" />
          Organização
        </h2>
        <p className="text-sm text-muted-foreground">
          Dados do titular da conta. Aparecem em notas fiscais e cobranças.
        </p>
      </header>

      <div className="rounded-xl border bg-card p-5 shadow-sm">
        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="org-name">Nome / Razão social</Label>
            <Input
              id="org-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Seu nome ou razão social"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="org-doc">CPF / CNPJ</Label>
            <Input
              id="org-doc"
              value={formatDoc(doc)}
              onChange={(e) => setDoc(e.target.value)}
              inputMode="numeric"
              placeholder="Apenas dígitos"
            />
          </div>
          <div className="flex justify-end">
            <Button onClick={save} disabled={saving}>
              {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
              Salvar
            </Button>
          </div>
        </div>
      </div>

      <div className="rounded-md border bg-muted/30 px-4 py-3 text-xs text-muted-foreground">
        Email da conta: <span className="font-medium text-foreground">{user?.email}</span>
      </div>
    </div>
  );
};