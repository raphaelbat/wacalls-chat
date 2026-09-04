import { useCallback, useEffect, useState } from "react";
import { Loader2, LogOut, Monitor, RefreshCw, Smartphone } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import { encerrarAcesso, listarAcessos, type Acesso } from "@/services/acessos";

/**
 * Quem está logado agora.
 *
 * Existe por uma pergunta prática de suporte: "essa senha está circulando?".
 * Cada linha é um navegador em um aparelho — e dá para derrubar na hora, sem
 * precisar trocar a senha de ninguém.
 */

// User-Agent é uma frase enorme e ilegível. O que interessa ao dono da conta é
// "Chrome no Windows", não a versão do WebKit.
const apelidoDoNavegador = (ua: string) => {
  const s = ua || "";
  const navegador =
    /Edg\//.test(s) ? "Edge"
    : /OPR\/|Opera/.test(s) ? "Opera"
    : /Chrome\//.test(s) ? "Chrome"
    : /Firefox\//.test(s) ? "Firefox"
    : /Safari\//.test(s) ? "Safari"
    : "Navegador";
  const sistema =
    /Windows/.test(s) ? "Windows"
    : /Android/.test(s) ? "Android"
    : /iPhone|iPad|iOS/.test(s) ? "iPhone"
    : /Mac OS/.test(s) ? "Mac"
    : /Linux/.test(s) ? "Linux"
    : "";
  return sistema ? `${navegador} no ${sistema}` : navegador;
};

const ehCelular = (ua: string) => /Android|iPhone|iPad|Mobile/i.test(ua || "");

const quando = (epoch: number) => {
  if (!epoch) return "—";
  const seg = Math.floor(Date.now() / 1000) - epoch;
  if (seg < 90) return "agora";
  if (seg < 3600) return `há ${Math.floor(seg / 60)} min`;
  if (seg < 86400) return `há ${Math.floor(seg / 3600)} h`;
  return new Date(epoch * 1000).toLocaleString("pt-BR", { dateStyle: "short", timeStyle: "short" });
};

export const AcessosAbertos = () => {
  const [acessos, setAcessos] = useState<Acesso[]>([]);
  const [carregando, setCarregando] = useState(true);
  const [encerrando, setEncerrando] = useState<Acesso | null>(null);

  const carregar = useCallback(async () => {
    setCarregando(true);
    try {
      setAcessos(await listarAcessos());
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Não consegui ler os acessos");
    } finally {
      setCarregando(false);
    }
  }, []);

  useEffect(() => {
    void carregar();
    const t = setInterval(() => void carregar(), 60_000);
    return () => clearInterval(t);
  }, [carregar]);

  const confirmarEncerrar = async () => {
    if (!encerrando) return;
    try {
      await encerrarAcesso(encerrando.token);
      toast.success("Acesso encerrado");
      setEncerrando(null);
      void carregar();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Não consegui encerrar");
    }
  };

  return (
    <div className="rounded-lg border">
      <div className="flex items-center justify-between gap-3 border-b px-4 py-3">
        <div>
          <h3 className="text-sm font-semibold">Quem está logado agora</h3>
          <p className="text-xs text-muted-foreground">
            Cada linha é um navegador. Entrar em outro navegador derruba o acesso anterior.
          </p>
        </div>
        <Button variant="ghost" size="sm" onClick={() => void carregar()} disabled={carregando}>
          {carregando ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
        </Button>
      </div>

      {carregando && acessos.length === 0 ? (
        <p className="px-4 py-6 text-center text-sm text-muted-foreground">Carregando…</p>
      ) : acessos.length === 0 ? (
        <p className="px-4 py-6 text-center text-sm text-muted-foreground">Nenhum acesso aberto.</p>
      ) : (
        <ul className="divide-y">
          {acessos.map((a) => {
            const Icone = ehCelular(a.navegador) ? Smartphone : Monitor;
            return (
              <li key={a.token} className="flex items-center gap-3 px-4 py-3">
                <Icone className="h-4 w-4 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium">
                    {a.nome || a.email}
                    {a.atual && (
                      <span className="ml-2 rounded bg-emerald-500/15 px-1.5 py-0.5 text-[11px] font-medium text-emerald-700 dark:text-emerald-300">
                        este navegador
                      </span>
                    )}
                  </p>
                  <p className="truncate text-xs text-muted-foreground">
                    {apelidoDoNavegador(a.navegador)} · {a.ip || "sem IP"} · visto {quando(a.visto)}
                  </p>
                </div>
                {!a.atual && (
                  <Button variant="ghost" size="sm" className="shrink-0 text-destructive" onClick={() => setEncerrando(a)}>
                    <LogOut className="mr-1 h-3.5 w-3.5" /> Encerrar
                  </Button>
                )}
              </li>
            );
          })}
        </ul>
      )}

      <ConfirmDialog
        open={!!encerrando}
        onOpenChange={(v) => !v && setEncerrando(null)}
        title="Encerrar este acesso?"
        description={
          encerrando
            ? `${encerrando.nome || encerrando.email} vai ser desconectado do ${apelidoDoNavegador(encerrando.navegador)} na hora.`
            : ""
        }
        confirmLabel="Encerrar"
        onConfirm={confirmarEncerrar}
      />
    </div>
  );
};
