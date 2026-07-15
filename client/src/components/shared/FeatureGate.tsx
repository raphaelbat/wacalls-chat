import type { ReactNode } from "react";
import { Lock } from "lucide-react";
import { usePlan } from "@/stores/plan";

type Props = {
  feature: string;
  children: ReactNode;
  // Quando true, esconde totalmente em vez de mostrar o aviso de upgrade.
  hideWhenBlocked?: boolean;
  fallback?: ReactNode;
};

// Renderiza children somente se o plano ativo libera a feature; caso contrário
// mostra um placeholder informando que é preciso fazer upgrade do plano.
export const FeatureGate = ({ feature, children, hideWhenBlocked, fallback }: Props) => {
  const { hasFeature } = usePlan();
  if (hasFeature(feature)) return <>{children}</>;
  if (hideWhenBlocked) return null;
  if (fallback) return <>{fallback}</>;
  return (
    <div className="flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed bg-muted/30 p-8 text-center">
      <Lock className="h-5 w-5 text-muted-foreground" />
      <p className="text-sm font-medium">Recurso não incluído no seu plano</p>
      <p className="text-xs text-muted-foreground">
        Faça upgrade para liberar <strong>{feature}</strong>.
      </p>
    </div>
  );
};