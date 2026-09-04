import { useEffect, type ReactNode } from "react";
import { Navigate, useLocation, useNavigate } from "react-router-dom";
import { clearAuthClientState, useAuth } from "@/stores/auth";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

export const RequireAuth = ({ children, adminOnly = false }: { children: ReactNode; adminOnly?: boolean }) => {
  const user = useAuth((s) => s.user);
  const loading = useAuth((s) => s.loading);
  const refresh = useAuth((s) => s.refresh);
  const loc = useLocation();
  const nav = useNavigate();

  useEffect(() => {
    if (loading) void refresh();
  }, [loading, refresh]);

  // Política de sessão única: se outro navegador fizer login com o mesmo
  // usuário, o backend revoga o token atual. Mantemos uma conexão SSE em
  // /api/auth/stream para receber o evento "revoked" em tempo real, além
  // de ouvir o evento "auth:invalidated" (disparado pelo cliente HTTP em
  // respostas 401) como fallback.
  useEffect(() => {
    if (!user) return;

    // Uma sessão derrubada gera 401 em TODAS as requisições que estavam no ar
    // (chats, licença, SSE, permissões...). Sem esta trava, cada uma abria um
    // aviso, e o cliente via a mesma frase empilhada quatro, cinco vezes.
    let jaAvisou = false;
    const encerrar = (motivo: "outro-acesso" | "expirou") => {
      if (jaAvisou) return;
      jaAvisou = true;
      toast.error(
        motivo === "outro-acesso"
          ? "Sua sessão foi encerrada porque esta conta entrou em outro navegador ou aparelho."
          : "Sua sessão expirou. Entre novamente.",
        { id: "sessao-encerrada" },
      );
      clearAuthClientState();
      useAuth.setState({ user: null });
      nav("/login", { replace: true });
    };

    // 401 solto só prova que o token não vale mais — pode ser expiração,
    // servidor reiniciado ou cookie perdido. Só o evento do servidor sabe
    // dizer que foi outro acesso.
    const handleInvalidated = () => encerrar("expirou");

    window.addEventListener("auth:invalidated", handleInvalidated);
    const es = new EventSource("/api/auth/stream", { withCredentials: true });
    es.addEventListener("revoked", () => {
      encerrar("outro-acesso");
      es.close();
    });
    es.onerror = () => {
      // O navegador reconecta sozinho; nada a fazer aqui.
    };
    return () => {
      window.removeEventListener("auth:invalidated", handleInvalidated);
      es.close();
    };
  }, [user, refresh, nav]);

  if (loading) {
    return (
      <div className="flex h-screen items-center justify-center">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }
  if (!user) {
    return <Navigate to="/login" replace state={{ from: loc.pathname }} />;
  }
  const isSuperAdmin = user.email.trim().toLowerCase() === "wacalls@admin.com";

  if (adminOnly && !user.roles.includes("admin") && !isSuperAdmin) {
    return <Navigate to="/" replace />;
  }
  return <>{children}</>;
};