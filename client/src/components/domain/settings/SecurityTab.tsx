import { useState } from "react";
import { KeyRound, Loader2, ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { updatePassword } from "@/services/auth";

export const SecurityTab = () => {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [saving, setSaving] = useState(false);

  const submit = async () => {
    if (!current || !next) {
      toast.error("Preencha a senha atual e a nova senha");
      return;
    }
    if (next.length < 8) {
      toast.error("A nova senha deve ter ao menos 8 caracteres");
      return;
    }
    if (next !== confirm) {
      toast.error("As senhas não conferem");
      return;
    }
    setSaving(true);
    try {
      await updatePassword(current, next);
      toast.success("Senha alterada");
      setCurrent("");
      setNext("");
      setConfirm("");
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="flex items-center gap-2 text-base font-semibold">
          <ShieldCheck className="h-4 w-4 text-primary" /> Segurança
        </h2>
        <p className="mt-1 text-sm text-muted-foreground">
          Altere sua senha de acesso. Use uma combinação forte de letras, números e símbolos.
        </p>
      </div>

      <div className="grid gap-4 sm:max-w-md">
        <div>
          <Label>Senha atual</Label>
          <Input
            type="password"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
            autoComplete="current-password"
          />
        </div>
        <div>
          <Label>Nova senha</Label>
          <Input
            type="password"
            value={next}
            onChange={(e) => setNext(e.target.value)}
            autoComplete="new-password"
          />
        </div>
        <div>
          <Label>Confirmar nova senha</Label>
          <Input
            type="password"
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            autoComplete="new-password"
          />
        </div>
      </div>

      <div className="flex justify-end">
        <Button onClick={submit} disabled={saving}>
          {saving ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <KeyRound className="h-4 w-4" />
          )}
          Alterar senha
        </Button>
      </div>
    </div>
  );
};