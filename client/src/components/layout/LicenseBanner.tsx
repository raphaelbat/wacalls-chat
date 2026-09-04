import { useEffect, useState } from "react";
import { CalendarClock, TriangleAlert } from "lucide-react";
import { useAuth } from "@/stores/auth";
import { getLicenseStatus, type LicenseStatus } from "@/services/license";

/**
 * Faixa de aviso de vencimento da licença.
 *
 * A renovação é automática: enquanto a mensalidade estiver em dia, o sistema
 * busca a licença nova sozinho e esta faixa nunca aparece. Ela só surge nos
 * últimos 7 dias — que é exatamente a janela em que o sistema já tentou
 * renovar e não conseguiu, ou seja, quando o pagamento falhou. Por isso não
 * dá para fechar: se sumir, o cliente descobre o problema quando o WhatsApp
 * parar de atender.
 */

const AVISO_A_PARTIR_DE = 7; // dias

const formataData = (epoch: number) =>
  new Date(epoch * 1000).toLocaleDateString("pt-BR", { day: "2-digit", month: "2-digit", year: "numeric" });

const soDigitos = (s: string) => s.replace(/\D/g, "");

export const LicenseBanner = () => {
  const user = useAuth((s) => s.user);
  const [st, setSt] = useState<LicenseStatus | null>(null);

  useEffect(() => {
    if (!user) return;
    let vivo = true;
    const ler = () => {
      getLicenseStatus()
        .then((s) => vivo && setSt(s))
        .catch(() => vivo && setSt(null));
    };
    ler();
    // O watcher do servidor tenta renovar a cada 12h; reler de hora em hora
    // faz a faixa sumir sozinha logo depois de o cliente pagar.
    const t = setInterval(ler, 60 * 60 * 1000);
    return () => {
      vivo = false;
      clearInterval(t);
    };
  }, [user]);

  if (!user || !st || !st.exigida || st.vitalicia) return null;
  if (st.diasParaVencer > AVISO_A_PARTIR_DE) return null;

  const venceu = st.diasParaVencer < 0;
  const dias = Math.max(st.diasParaVencer, 0);
  const zap = `https://wa.me/${soDigitos("55" + st.suporte)}`;

  const titulo = venceu
    ? st.emTolerancia
      ? `Sua licença venceu — o sistema para em ${st.diasTolerancia} dia${st.diasTolerancia === 1 ? "" : "s"}`
      : "Sua licença venceu"
    : dias === 0
      ? "Sua licença vence hoje"
      : `Sua licença vence em ${dias} dia${dias === 1 ? "" : "s"}`;

  // A frase precisa dizer DE QUE é a mensalidade: é do WaCalls Local, o
  // instalador desta máquina. Sem isso o cliente acha que é cobrança do
  // WhatsApp, de servidor, ou de algum serviço na nuvem.
  const detalhe = venceu
    ? "A assinatura mensal do WaCalls Local (o instalador deste computador) venceu. Refaça o pagamento na Kiwify: assim que a cobrança for aprovada, o sistema se reativa sozinho."
    : `Válida até ${st.expiraEm ? formataData(st.expiraEm) : "—"}. É a assinatura mensal do WaCalls Local, o instalador deste computador; com a mensalidade em dia a renovação é automática e este aviso some sozinho.`;

  const Icone = venceu ? TriangleAlert : CalendarClock;
  const cor = venceu
    ? "border-red-500/30 bg-red-500/10 text-red-500"
    : "border-amber-500/30 bg-amber-500/10 text-amber-500";
  const botao = venceu ? "bg-red-500 hover:bg-red-600" : "bg-amber-500 hover:bg-amber-600";

  return (
    <div className={`mx-4 mt-3 flex items-center gap-3 rounded-lg border px-4 py-3 sm:mx-6 ${cor}`}>
      <Icone className="h-5 w-5 shrink-0" />
      <div className="flex-1 text-sm text-foreground">
        <span className="font-semibold">{titulo}</span>{" "}
        <span className="text-muted-foreground">{detalhe}</span>
      </div>
      <a
        href={zap}
        target="_blank"
        rel="noopener noreferrer"
        className={`shrink-0 rounded-md px-3 py-1.5 text-sm font-medium text-white ${botao}`}
      >
        Falar com o suporte
      </a>
    </div>
  );
};
