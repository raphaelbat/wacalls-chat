import { useEffect, useState } from "react";
import { CreditCard, ExternalLink, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { formatBRL } from "@/lib/instance-plan";
import { createCheckout } from "@/services/billing";
import { getActivePlan, listPlans } from "@/services/settings";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  sessionId: string;
  sessionName: string;
  price: number;
};

// UI-only payment dialog. Real Stripe/checkout integration is intentionally
// not wired here — the user explicitly opted out of cloud-hosted billing.
// Clicking "Pagar Fatura" marks the instance as paid in localStorage so the
// rest of the UI can flow naturally and the merchant can replace this with a
// real provider later.
export const PaymentDialog = ({ open, onOpenChange, sessionId, sessionName, price }: Props) => {
  const now = new Date();
  const month = now.toLocaleDateString("pt-BR", { month: "long", year: "numeric" });
  const [loading, setLoading] = useState(false);
  // Always reflect the price configured in Settings → Plans. Falls back to the
  // prop only while the plan list is loading or no active plan was published.
  const [planPrice, setPlanPrice] = useState<number | null>(null);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    (async () => {
      try {
        const active = await getActivePlan().catch(() => null);
        let valor = active?.plan?.valor;
        if (valor == null) {
          const plans = await listPlans().catch(() => []);
          valor = plans[0]?.valor;
        }
        if (!cancelled && typeof valor === "number") setPlanPrice(valor);
      } catch {
        /* fallback to prop */
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [open]);

  const effectivePrice = planPrice ?? price;

  const onPay = async () => {
    setLoading(true);
    try {
      const { url } = await createCheckout(1);
      if (!url) {
        toast.error(
          "Stripe não retornou URL de checkout. Verifique a Secret Key em Configurações → SaaS → Pagamentos.",
        );
        return;
      }
      // Sinaliza ao Billing para iniciar polling de sincronização caso
      // o checkout abra em nova aba ou o webhook chegue antes do retorno.
      try { window.dispatchEvent(new Event("billing:refresh")); } catch { /* noop */ }
      try { localStorage.setItem("billing:lastCheckoutAt", String(Date.now())); } catch { /* noop */ }
      window.location.href = url;
    } catch (e) {
      const msg = (e as Error)?.message || "Falha ao iniciar pagamento";
      // eslint-disable-next-line no-console
      console.error("[billing] checkout error", e, { sessionId });
      toast.error(msg, { duration: 8000 });
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md gap-0 p-0">
        <div className="flex items-center justify-between border-b px-5 py-3.5">
          <div className="flex items-center gap-2 text-sm font-semibold">
            <CreditCard className="h-4 w-4 text-primary" />
            Pagamento Necessário
          </div>
        </div>

        <div className="space-y-4 px-5 py-5">
          <p className="text-sm leading-relaxed text-foreground/80">
            Para conectar esta instância, é necessário realizar o pagamento
            da fatura mensal. Você será redirecionado para o checkout seguro.
          </p>

          <div className="rounded-lg border bg-muted/30 p-4 text-sm">
            <div className="flex items-center justify-between py-1">
              <span className="text-muted-foreground">Instância</span>
              <span className="font-semibold">{sessionName}</span>
            </div>
            <div className="flex items-center justify-between py-1">
              <span className="text-muted-foreground">Referência</span>
              <span className="font-semibold">{month}</span>
            </div>
            <div className="mt-2 border-t pt-2">
              <div className="flex items-center justify-between">
                <span className="font-semibold">Total</span>
                <span className="text-lg font-bold text-primary">{formatBRL(effectivePrice)}</span>
              </div>
            </div>
          </div>

          <Button onClick={onPay} className="w-full" size="lg" disabled={loading}>
            {loading ? (
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            ) : (
              <ExternalLink className="mr-2 h-4 w-4" />
            )}
            {loading ? "Abrindo checkout…" : "Pagar Fatura"}
          </Button>

          <p className="text-center text-[11px] text-muted-foreground">
            Após o pagamento, a instância será liberada automaticamente em instantes.
          </p>
        </div>
      </DialogContent>
    </Dialog>
  );
};
