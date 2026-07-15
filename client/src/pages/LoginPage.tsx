import { useEffect, useState } from "react";
import { Navigate, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import {
  PhoneCall,
  Mail,
  Lock,
  Eye,
  EyeOff,
  Loader2,
  MessageCircle,
  BarChart3,
  ShieldCheck,
  Sparkles,
} from "lucide-react";
import { useAuth } from "@/stores/auth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ForgotPasswordDialog } from "@/components/auth/ForgotPasswordDialog";
// OBS: Este projeto é Open Source. Sinta-se livre para usar, modificar e contribuir.
// Distribuído sob licença open source — código aberto para a comunidade.
export const LoginPage = () => {
  const user = useAuth((s) => s.user);
  const loading = useAuth((s) => s.loading);
  const login = useAuth((s) => s.login);
  const refresh = useAuth((s) => s.refresh);
  const [email, setEmail] = useState("wacalls@admin.com");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [forgotOpen, setForgotOpen] = useState(false);
  const navigate = useNavigate();

  useEffect(() => {
    void refresh();
  }, [refresh]);

  if (user) return <Navigate to="/chats" replace />;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!email || !password) return;
    setSubmitting(true);
    try {
      await login(email.trim(), password);
      toast.success("Bem-vindo!");
      navigate("/chats", { replace: true });
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error("Falha no login", { description: msg });
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="relative min-h-dvh overflow-hidden bg-background">
      {/* Ambient background */}
      <div className="pointer-events-none absolute inset-0 -z-10">
        <div className="absolute -top-24 -left-24 h-[420px] w-[420px] rounded-full bg-primary/25 blur-3xl" />
        <div className="absolute top-1/3 -right-32 h-[520px] w-[520px] rounded-full bg-emerald-500/20 blur-3xl" />
        <div className="absolute bottom-0 left-1/3 h-[380px] w-[380px] rounded-full bg-sky-500/15 blur-3xl" />
      </div>

      <div className="mx-auto grid min-h-dvh w-full max-w-6xl grid-cols-1 items-center gap-8 px-4 py-8 lg:grid-cols-2 lg:gap-12 lg:px-8">
        {/* Brand / marketing panel */}
        <aside className="relative hidden overflow-hidden rounded-3xl border bg-gradient-to-br from-primary/90 via-emerald-500/80 to-teal-600/90 p-10 text-primary-foreground shadow-2xl lg:flex lg:h-[640px] lg:flex-col lg:justify-between">
          <div className="pointer-events-none absolute inset-0 opacity-30 mix-blend-overlay [background-image:radial-gradient(circle_at_20%_20%,white_0,transparent_40%),radial-gradient(circle_at_80%_60%,white_0,transparent_35%)]" />

          <div className="relative z-10 flex items-center gap-3">
            <div className="grid h-11 w-11 place-items-center rounded-xl bg-white/15 backdrop-blur">
              <PhoneCall className="h-5 w-5" />
            </div>
            <div>
              <p className="text-lg font-semibold tracking-tight">Calls</p>
              <p className="text-xs opacity-80">Atendimento &amp; chamadas em um só lugar</p>
            </div>
          </div>

          <div className="relative z-10 space-y-5">
            <h2 className="text-3xl font-semibold leading-tight tracking-tight">
              Converse, ligue e converta — <span className="opacity-80">tudo em uma única tela.</span>
            </h2>
            <ul className="space-y-3 text-sm">
              <li className="flex items-start gap-3">
                <MessageCircle className="mt-0.5 h-4 w-4 opacity-90" />
                <span>WhatsApp multi-conexão com filas, tags e histórico completo.</span>
              </li>
              <li className="flex items-start gap-3">
                <PhoneCall className="mt-0.5 h-4 w-4 opacity-90" />
                <span>Chamadas VoIP integradas ao contato — sem trocar de app.</span>
              </li>
              <li className="flex items-start gap-3">
                <BarChart3 className="mt-0.5 h-4 w-4 opacity-90" />
                <span>Relatórios em tempo real de atendimentos, filas e ligações.</span>
              </li>
              <li className="flex items-start gap-3">
                <ShieldCheck className="mt-0.5 h-4 w-4 opacity-90" />
                <span>Sessões criptografadas e controle de acesso por perfil.</span>
              </li>
            </ul>
          </div>

          <div className="relative z-10 flex items-center gap-2 text-xs opacity-80">
            <Sparkles className="h-3.5 w-3.5" />
            <span>Calls — Gratuito</span>
          </div>
        </aside>

        {/* Login card */}
        <div className="mx-auto w-full max-w-md">
          <div className="rounded-2xl border bg-card/80 p-8 shadow-xl backdrop-blur-xl">
            <div className="mb-6 flex flex-col items-center gap-3 text-center lg:hidden">
              <div className="grid h-14 w-14 place-items-center rounded-2xl bg-primary text-primary-foreground shadow-lg shadow-primary/30">
                <PhoneCall className="h-6 w-6" />
              </div>
              <div>
                <h1 className="text-2xl font-semibold tracking-tight">Calls</h1>
                <p className="text-sm text-muted-foreground">Entre para acessar sua conta</p>
              </div>
            </div>

            <div className="mb-6 hidden text-center lg:block">
              <h1 className="text-2xl font-semibold tracking-tight">Entrar na sua conta</h1>
              <p className="mt-1 text-sm text-muted-foreground">
                Use suas credenciais para acessar o painel.
              </p>
            </div>

            <form onSubmit={handleSubmit} className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="email">Email</Label>
                <div className="relative">
                  <Mail className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    id="email"
                    type="email"
                    autoComplete="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    required
                    className="h-11 pl-9"
                    placeholder="voce@empresa.com"
                  />
                </div>
              </div>

              <div className="space-y-2">
                <Label htmlFor="password">Senha</Label>
                <div className="relative">
                  <Lock className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    id="password"
                    type={showPassword ? "text" : "password"}
                    autoComplete="current-password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    required
                    className="h-11 pl-9 pr-10"
                    placeholder="••••••••"
                  />
                  <button
                    type="button"
                    onClick={() => setShowPassword((v) => !v)}
                    className="absolute right-2 top-1/2 -translate-y-1/2 rounded-md p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground"
                    aria-label={showPassword ? "Ocultar senha" : "Mostrar senha"}
                  >
                    {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </button>
                </div>
              </div>

              <Button
                type="submit"
                className="h-11 w-full text-base font-medium shadow-lg shadow-primary/20"
                disabled={submitting || loading}
              >
                {submitting ? (
                  <>
                    <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                    Entrando...
                  </>
                ) : (
                  "Entrar"
                )}
              </Button>
            </form>

            <p className="mt-6 text-center text-xs text-muted-foreground">
              Ao entrar, você concorda com os Termos de uso e a Política de privacidade.
            </p>
          </div>

          <p className="mt-4 text-center text-xs text-muted-foreground">
            © {new Date().getFullYear()} Calls · Todos os direitos reservados
          </p>
          <p className="mt-1 text-center text-xs font-medium text-primary">
            Este é um projeto Open Source 💚
          </p>
        </div>
      </div>

      <ForgotPasswordDialog open={forgotOpen} onOpenChange={setForgotOpen} />
    </div>
  );
};

export default LoginPage;
