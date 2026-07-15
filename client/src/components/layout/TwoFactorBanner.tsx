import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { ShieldAlert, X } from "lucide-react";
import { useAuth } from "@/stores/auth";
import * as twofa from "@/services/twofa";

const DISMISS_KEY = "vozzap.2fa.bannerDismissed";

export const TwoFactorBanner = () => {
  const user = useAuth((s) => s.user);
  const isAdmin = !!user?.roles.includes("admin");
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [dismissed, setDismissed] = useState<boolean>(() => {
    try { return sessionStorage.getItem(DISMISS_KEY) === "1"; } catch { return false; }
  });

  useEffect(() => {
    if (!user || !isAdmin) return;
    twofa.getStatus(user.id).then((s) => setEnabled(s.enabled)).catch(() => setEnabled(false));
  }, [user, isAdmin]);

  if (!user || !isAdmin || dismissed || enabled !== false) return null;

  const close = () => {
    setDismissed(true);
    try { sessionStorage.setItem(DISMISS_KEY, "1"); } catch { /* ignore */ }
  };

  return (
    <div className="mx-4 mt-3 flex items-center gap-3 rounded-lg border border-orange-500/30 bg-orange-500/10 px-4 py-3 sm:mx-6">
      <ShieldAlert className="h-5 w-5 shrink-0 text-orange-500" />
      <div className="flex-1 text-sm">
        <span className="font-semibold">Ative a autenticação em 2 fatores</span>{" "}
        <span className="text-muted-foreground">Segurança extra com código do celular.</span>
      </div>
      <Link
        to="/2fa"
        className="rounded-md bg-orange-500 px-3 py-1.5 text-sm font-medium text-white hover:bg-orange-600"
      >
        Ativar
      </Link>
      <button
        onClick={close}
        aria-label="Fechar"
        className="rounded p-1 text-muted-foreground hover:bg-orange-500/10 hover:text-foreground"
      >
        <X className="h-4 w-4" />
      </button>
    </div>
  );
};
