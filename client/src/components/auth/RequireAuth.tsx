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
    const handleInvalidated = () => {
      toast.error("Sua sessão foi encerrada porque você entrou em outro navegador.");
      clearAuthClientState();
      useAuth.setState({ user: null });
      nav("/login", { replace: true });
    };
    window.addEventListener("auth:invalidated", handleInvalidated);
    const es = new EventSource("/api/auth/stream", { withCredentials: true });
    es.addEventListener("revoked", () => {
      handleInvalidated();
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
  if (adminOnly && !user.roles.includes("admin")) {
    return <Navigate to="/" replace />;
  }
  return <>{children}</>;
};