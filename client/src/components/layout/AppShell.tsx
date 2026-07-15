import { createContext, useContext, useEffect, useMemo, useState, type ReactNode, type ComponentType } from "react";
import { BarChart3, ChevronDown, ChevronsLeft, ChevronsRight, Contact2, History, KanbanSquare, Layers, Maximize2, Megaphone, Menu as MenuIcon, MessageSquare, Minimize2, PhoneCall, Radio, Settings, ShoppingCart, Tag, Users2, Wifi, Workflow } from "lucide-react";
import { Link, useLocation } from "react-router-dom";
import { NotificationsMenu } from "./NotificationsMenu";
import { UserMenu } from "./UserMenu";
import { IncomingCallModal } from "./IncomingCallModal";

import { DialerTriggerButton } from "./DialerTriggerButton";
import { DialerPanel } from "./DialerPanel";
import { ThemeToggle } from "./ThemeToggle";
import { useAuth } from "@/stores/auth";
import { useChats } from "@/stores/chats";
import { ensureCallsWired } from "@/stores/calls";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { hasPermission, type Permission } from "@/lib/permissions";
import { usePlan, usePlanStore } from "@/stores/plan";
import { useOptionsStore } from "@/stores/options";
import * as settingsApi from "@/services/settings";
import { subscribeWhitelabel, readCachedWhitelabel } from "@/lib/whitelabel";
import { useTheme } from "@/stores/theme";
import { useFreeTierAlerts } from "@/hooks/useFreeTierAlerts";
import { useTranslation } from "react-i18next";
import { LanguageSwitcher } from "./LanguageSwitcher";

/**
 * When AppShell is rendered inside another AppShell (e.g. the unified Settings
 * page embedding existing pages), the inner shell becomes a transparent
 * passthrough so the sidebar/header are not duplicated.
 */
export const EmbeddedShellContext = createContext(false);

export const AppShell = ({ children }: { children: ReactNode }) => {
  const embedded = useContext(EmbeddedShellContext);
  if (embedded) return <>{children}</>;
  return <AppShellInner>{children}</AppShellInner>;
};

const AppShellInner = ({ children }: { children: ReactNode }) => {
  const loc = useLocation();
  const { t } = useTranslation();
  const user = useAuth((s) => s.user);
  useFreeTierAlerts();
  const theme = useTheme((s) => s.theme);
  const [wl, setWl] = useState<settingsApi.Whitelabel | null>(
    () => (readCachedWhitelabel() as settingsApi.Whitelabel | null) ?? null,
  );
  useEffect(() => {
    let alive = true;
    settingsApi
      .getWhitelabel()
      .then((v) => {
        if (!alive) return;
        // Mescla com o cache para nunca regredir a logo/favicon quando o
        // backend devolver um payload parcial.
        setWl((prev) => ({ ...(prev || {}), ...v } as settingsApi.Whitelabel));
      })
      .catch(() => {});
    const off = subscribeWhitelabel((v) => setWl(v));
    return () => { alive = false; off(); };
  }, []);
  const isDark = theme === "dark" || (typeof document !== "undefined" && document.documentElement.classList.contains("dark"));
  const brandLogo = (isDark ? wl?.logoDark : wl?.logoLight) || wl?.logoLight || wl?.logoDark;
  const brandName = wl?.appName || "VozZap";
  const isAdmin = !!user?.roles.includes("admin");
  // Settings (planos, whitelabel, opções, empresas globais) é exclusivo
  // do administrador do SaaS — outras empresas/admin não veem o item.
  const isSuperAdmin =
    !!user &&
    isAdmin &&
    user.email.trim().toLowerCase() === "admin@equipechat.com";
  // Carrega o plano ativo assim que o shell monta para que itens não cobertos
  // pelo plano fiquem ocultos imediatamente.
  const planLoaded = usePlanStore((s) => s.loaded);
  const loadPlan = usePlanStore((s) => s.load);
  useEffect(() => { if (user && !planLoaded) void loadPlan(); }, [user, planLoaded, loadPlan]);
  const optsLoaded = useOptionsStore((s) => s.loaded);
  const loadOpts = useOptionsStore((s) => s.load);
  useEffect(() => { if (user && !optsLoaded) void loadOpts(); }, [user, optsLoaded, loadOpts]);
  // Ativa o listener SSE que popula `incoming` no store de chamadas —
  // sem isso, o modal "Incoming Call" jamais aparece quando o cliente liga.
  useEffect(() => { if (user) ensureCallsWired(); }, [user]);
  const { hasFeature } = usePlan();
  const [collapsed, setCollapsed] = useState<boolean>(() => {
    try { return localStorage.getItem("primevoip.sidebar.collapsed") === "1"; } catch { return false; }
  });
  const toggleCollapsed = () => {
    setCollapsed((v) => {
      const next = !v;
      try { localStorage.setItem("primevoip.sidebar.collapsed", next ? "1" : "0"); } catch { /* noop */ }
      return next;
    });
  };
  const [mobileOpen, setMobileOpen] = useState(false);
  // Auto-close the mobile drawer on route change for predictable nav UX.
  useEffect(() => { setMobileOpen(false); }, [loc.pathname]);

  // Fullscreen toggle (Flowkit-style header action)
  const [isFs, setIsFs] = useState<boolean>(typeof document !== "undefined" && !!document.fullscreenElement);
  useEffect(() => {
    const onChange = () => setIsFs(!!document.fullscreenElement);
    document.addEventListener("fullscreenchange", onChange);
    return () => document.removeEventListener("fullscreenchange", onChange);
  }, []);
  const toggleFullscreen = () => {
    if (document.fullscreenElement) document.exitFullscreen().catch(() => {});
    else document.documentElement.requestFullscreen().catch(() => {});
  };

  // Aggregate unread chat count across all paired sessions to badge the nav entry.
  const chatsBySession = useChats((s) => s.chatsBySession);
  const unreadTotal = useMemo(() => {
    let n = 0;
    for (const list of Object.values(chatsBySession)) for (const c of list) if ((c.unread ?? 0) > 0) n += 1;
    return n;
  }, [chatsBySession]);

  // `feat` mapeia o item de menu para o rótulo do recurso no plano. Quando
  // o plano ativo não inclui o recurso, o item fica oculto para todos
  // (admin inclusive, para refletir os limites contratados).
  // Subitens de Campanhas: Voz (URA) e Broadcast — cada um gateado por
  // seu próprio recurso para que o plano controle individualmente.
  // Menu enxuto: apenas Chat, Conexões e Usuários (admin).
  const campaignChildren: (NavItem & { feat?: string })[] = [];
  const principal: (NavItem & { perm: Permission; feat?: string })[] = [
    { to: "/chats", icon: MessageSquare, label: t("nav.chats", { defaultValue: "Chat" }), badge: unreadTotal, perm: "chats" },
    { to: "/contacts", icon: Contact2, label: t("nav.contacts", { defaultValue: "Contatos" }), perm: "chats" },
    { to: "/queues", icon: Users2, label: t("nav.queues", { defaultValue: "Filas" }), perm: "chats" },
    { to: "/kanban", icon: KanbanSquare, label: t("nav.kanban", { defaultValue: "Kanban" }), perm: "chats" },
    { to: "/connections", icon: Wifi, label: t("nav.connections", { defaultValue: "Conexões" }), perm: "connections" },
    { to: "/reports", icon: BarChart3, label: t("nav.reports", { defaultValue: "Relatórios" }), perm: "chats" },
  ];
  const aplicacoes: (NavItem & { perm: Permission; feat?: string })[] = [];
  if (isAdmin) {
    aplicacoes.push({ to: "/admin/users", icon: Contact2, label: t("nav.users", { defaultValue: "Usuários" }), perm: "chats" });
  }
  const filterAllowed = (arr: (NavItem & { perm: Permission; feat?: string })[]): NavItem[] =>
    arr.filter((it) => hasPermission(user, it.perm));
  const principalItems = filterAllowed(principal);
  const aplicacoesItems = filterAllowed(aplicacoes);
  const settingsItem: NavItem = { to: "/chats", icon: MessageSquare, label: t("nav.chats", { defaultValue: "Chat" }) };

  const isActive = (to: string) => loc.pathname === to || loc.pathname.startsWith(to + "/");

  // Page title + breadcrumb shown on the left of the header (Flowkit-style).
  const pageMeta = useMemo(() => {
    const all = [...principal, ...aplicacoes, ...campaignChildren];
    // exact match first
    const exact = all.find((it) => it.to === loc.pathname);
    if (exact) return { title: exact.label, parent: brandName };
    const startsWith = all.find((it) => loc.pathname.startsWith(it.to + "/")) || all.find((it) => loc.pathname.startsWith(it.to));
    if (startsWith) return { title: startsWith.label, parent: brandName };
    // Fallback titles for routes not present in the side menu.
    const fallbacks: Array<{ match: (p: string) => boolean; label: string }> = [
      { match: (p) => p.startsWith("/users"), label: t("nav.users") || "Usuários" },
      { match: (p) => p.startsWith("/queues"), label: t("nav.queues") || "Filas" },
      { match: (p) => p.startsWith("/tags"), label: t("nav.tags") || "Tags" },
      { match: (p) => p.startsWith("/agents"), label: t("nav.agents") || "Agentes IA" },
      { match: (p) => p.startsWith("/api"), label: t("nav.api") || "Documentação" },
      { match: (p) => p.startsWith("/profile"), label: t("nav.profile") || "Perfil" },
      { match: (p) => p.startsWith("/user-settings"), label: t("nav.userSettings") || "Configurações do Usuário" },
      { match: (p) => p.startsWith("/companies"), label: t("nav.companies") || "Empresas" },
      { match: (p) => p.startsWith("/connections"), label: t("nav.connections") || "Conexões" },
      { match: (p) => p.startsWith("/contacts"), label: t("nav.contacts") || "Contatos" },
      { match: (p) => p.startsWith("/chats"), label: t("nav.chats") || "Conversas" },
      { match: (p) => p.startsWith("/flows"), label: t("nav.flows") || "Flowbuilder" },
      { match: (p) => p.startsWith("/kanban"), label: t("nav.kanban") || "Pipeline" },
      { match: (p) => p.startsWith("/history"), label: t("nav.history") || "Histórico" },
      { match: (p) => p.startsWith("/reports"), label: t("nav.reports") || "Relatórios" },
      { match: (p) => p.startsWith("/billing"), label: t("nav.billing") || "Financeiro" },
      { match: (p) => p.startsWith("/settings"), label: t("nav.settings") || "Configurações" },
    ];
    const fb = fallbacks.find((f) => f.match(loc.pathname));
    if (fb) return { title: fb.label, parent: "" };
    return { title: "", parent: "" };
  }, [loc.pathname, brandName, t, unreadTotal]);

  const renderSidebar = (forMobile = false) => (
    <div className="relative flex h-full flex-col">
      <div className={`relative flex h-20 items-center border-b overflow-hidden ${collapsed && !forMobile ? "justify-center px-1" : "justify-between gap-2 px-3 py-2"}`}>
        <div className="flex min-w-0 flex-1 items-center justify-center overflow-hidden">
          {brandLogo ? (
            <img
              src={brandLogo}
              alt={brandName}
              className={
                collapsed && !forMobile
                  ? "block h-full max-h-12 w-auto object-contain"
                  : "block max-h-14 w-auto max-w-full object-contain"
              }
            />
          ) : (
            <span className={`flex shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground ${collapsed && !forMobile ? "h-7 w-7" : "h-10 w-10"}`}>
              <PhoneCall className={collapsed && !forMobile ? "h-3.5 w-3.5" : "h-5 w-5"} />
            </span>
          )}
        </div>
        {!forMobile && (
          <button
            type="button"
            onClick={toggleCollapsed}
            aria-label={collapsed ? t("nav.expand") : t("nav.collapse")}
            className={`shrink-0 grid place-items-center rounded-full border border-border/60 bg-muted/60 text-muted-foreground transition-colors hover:bg-primary/10 hover:text-primary ${collapsed ? "absolute -right-3 top-1/2 -translate-y-1/2 h-7 w-7 bg-background shadow-sm" : "h-8 w-8"}`}
          >
            {collapsed ? <ChevronsRight className="h-4 w-4" /> : <ChevronsLeft className="h-4 w-4" />}
          </button>
        )}
      </div>

      <nav className="flex-1 overflow-y-auto px-3 py-4 relative z-10" aria-label={t("nav.menu")}>
        {(!collapsed || forMobile) && (
          <div className="px-2 pb-2 text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground/80">
            {t("nav.principal", { defaultValue: "Principal" })}
          </div>
        )}
        <ul className="flex flex-col gap-1">
          {principalItems.map((it) => (
            <NavRow key={it.to} item={it} active={isActive(it.to)} collapsed={collapsed && !forMobile} isActive={isActive} />
          ))}
        </ul>

        {aplicacoesItems.length > 0 && (
          <>
            {(!collapsed || forMobile) && (
              <div className="px-2 pb-2 pt-6 text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground/80">
                {t("nav.apps", { defaultValue: "Aplicações" })}
              </div>
            )}
            <ul className="flex flex-col gap-1">
              {aplicacoesItems.map((it) => (
                <NavRow key={it.to} item={it} active={isActive(it.to)} collapsed={collapsed && !forMobile} isActive={isActive} />
              ))}
            </ul>
          </>
        )}
      </nav>

      {/* Decorative animated wave/dot pattern – funciona em ambos os temas */}
      <SidebarBackdrop collapsed={collapsed && !forMobile} />

      <div className="relative z-10 border-t px-2 py-2 space-y-1 bg-background/80 backdrop-blur-sm" />

    </div>
  );

  return (
    <div className="flex h-dvh w-full overflow-hidden bg-muted/30">
      {/* Desktop sidebar */}
      <aside
        className={`sticky top-0 z-30 hidden h-dvh shrink-0 flex-col border-r bg-background transition-[width] duration-200 md:flex ${collapsed ? "w-16" : "w-60"}`}
        aria-label={t("nav.menu")}
      >
        {renderSidebar(false)}
      </aside>

      {/* Mobile drawer */}
      <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
        <SheetContent side="left" className="w-72 p-0">
          <SheetTitle className="sr-only">{t("nav.menu")}</SheetTitle>
          {renderSidebar(true)}
        </SheetContent>
      </Sheet>

      <div className="flex h-dvh min-w-0 flex-1 flex-col overflow-hidden">
        <header className="flex h-16 shrink-0 items-center gap-2 border-b bg-background/80 px-3 backdrop-blur sm:px-6">
          <Button
            variant="ghost"
            size="icon"
            className="md:hidden"
            onClick={() => setMobileOpen(true)}
            aria-label={t("nav.openMenu")}
          >
            <MenuIcon className="h-5 w-5" />
          </Button>
          <h1 className="ml-1 truncate text-base font-semibold tracking-tight sm:text-lg">
            {pageMeta.title}
          </h1>
          <div className="ml-auto flex items-center gap-1.5">
            <LanguageSwitcher />
            {user && <NotificationsMenu />}
            <Button variant="ghost" size="icon" aria-label="Tela cheia" className="hidden sm:inline-flex" onClick={toggleFullscreen}>
              {isFs ? <Minimize2 className="h-4.5 w-4.5" /> : <Maximize2 className="h-4.5 w-4.5" />}
            </Button>
            <ThemeToggle />
            {user && <DialerTriggerButton />}
            {user && <UserMenu />}
          </div>
        </header>
        
        <main className={`min-h-0 flex-1 px-4 sm:px-6 ${loc.pathname.startsWith("/chats") ? "overflow-hidden py-3" : "overflow-auto py-5"}`}>
          {children}
        </main>
      </div>
      {user && <DialerPanel />}
      {user && <IncomingCallModal />}
    </div>
  );
};

type NavItem = {
  to: string;
  icon: ComponentType<{ className?: string }>;
  label: string;
  badge?: number;
  children?: NavItem[];
};

// Per-route color palette para os "icon pills" – usa apenas classes
// Tailwind seguras de purge (literais) e funciona em ambos os temas.
const ICON_TONES: Record<string, { bg: string; fg: string; ring: string }> = {
  "/reports":      { bg: "bg-blue-500/15",    fg: "text-blue-500",    ring: "ring-blue-500/30" },
  "/chats":        { bg: "bg-emerald-500/15", fg: "text-emerald-500", ring: "ring-emerald-500/30" },
  "/contacts":     { bg: "bg-indigo-500/15",  fg: "text-indigo-400",  ring: "ring-indigo-500/30" },
  "/connections":  { bg: "bg-cyan-500/15",    fg: "text-cyan-400",    ring: "ring-cyan-500/30" },
  "/history":      { bg: "bg-amber-500/15",   fg: "text-amber-400",   ring: "ring-amber-500/30" },
  "/flows":        { bg: "bg-emerald-500/15", fg: "text-emerald-400", ring: "ring-emerald-500/30" },
  "/queues":       { bg: "bg-violet-500/15",  fg: "text-violet-400",  ring: "ring-violet-500/30" },
  "/tags":         { bg: "bg-pink-500/15",    fg: "text-pink-400",    ring: "ring-pink-500/30" },
  "/kanban":       { bg: "bg-orange-500/15",  fg: "text-orange-400",  ring: "ring-orange-500/30" },
  "/campaigns":    { bg: "bg-rose-500/15",    fg: "text-rose-400",    ring: "ring-rose-500/30" },
  "/cart-recovery":{ bg: "bg-lime-500/15",    fg: "text-lime-500",    ring: "ring-lime-500/30" },
  "/settings":     { bg: "bg-slate-500/15",   fg: "text-slate-400",   ring: "ring-slate-500/30" },
  "/admin/settings":{ bg: "bg-slate-500/15",  fg: "text-slate-400",   ring: "ring-slate-500/30" },
};
const DEFAULT_TONE = { bg: "bg-primary/15", fg: "text-primary", ring: "ring-primary/30" };
const toneFor = (to: string) => ICON_TONES[to] || DEFAULT_TONE;

const NavRow = ({ item, active, collapsed, isActive }: { item: NavItem; active: boolean; collapsed: boolean; isActive?: (to: string) => boolean }) => {
  const Icon = item.icon;
  const hasChildren = !!item.children?.length;
  const childActive = hasChildren && !!item.children!.some((c) => isActive?.(c.to));
  const [open, setOpen] = useState<boolean>(childActive);
  useEffect(() => { if (childActive) setOpen(true); }, [childActive]);
  const tone = toneFor(item.to);
  const iconPill = (highlighted: boolean) => (
    <span
      className={`grid place-items-center rounded-lg transition-colors ${collapsed ? "h-9 w-9" : "h-8 w-8"} ${
        highlighted ? "bg-primary/20 ring-1 ring-primary/40 text-primary" : `${tone.bg} ${tone.fg} ring-1 ${tone.ring}`
      }`}
    >
      <Icon className="h-4 w-4" />
    </span>
  );
  if (hasChildren && !collapsed) {
    return (
      <li>
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          className={`group relative flex w-full min-h-11 items-center gap-3 rounded-xl px-2 py-1.5 text-sm font-medium transition-all duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
            childActive
              ? "bg-primary/10 text-foreground ring-1 ring-primary/30 shadow-sm"
              : "text-foreground/80 hover:bg-muted/70 hover:text-foreground"
          }`}
        >
          {iconPill(childActive)}
          <span className="flex-1 truncate text-left">{item.label}</span>
          <ChevronDown className={`h-3.5 w-3.5 shrink-0 transition-transform ${open ? "rotate-180" : ""}`} />
        </button>
        {open && (
          <ul className="mt-0.5 flex flex-col gap-0.5 pl-11">
            {item.children!.map((child) => {
              const ChildIcon = child.icon;
              const cActive = !!isActive?.(child.to);
              return (
                <li key={child.to}>
                  <Link
                    to={child.to}
                    aria-current={cActive ? "page" : undefined}
                    className={`flex min-h-8 items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                      cActive ? "text-primary font-semibold" : "text-muted-foreground hover:bg-muted hover:text-foreground"
                    }`}
                  >
                    <ChildIcon className="h-3.5 w-3.5 shrink-0" />
                    <span className="flex-1 truncate">{child.label}</span>
                  </Link>
                </li>
              );
            })}
          </ul>
        )}
      </li>
    );
  }
  return (
    <li>
      <Link
        to={item.to}
        title={collapsed ? item.label : undefined}
        aria-current={active ? "page" : undefined}
        className={`group relative flex min-h-11 items-center gap-3 rounded-xl px-2 py-1.5 text-sm font-medium transition-all duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
          active
            ? "bg-primary/10 text-foreground ring-1 ring-primary/30 shadow-sm"
            : "text-foreground/80 hover:bg-muted/70 hover:text-foreground"
        } ${collapsed ? "justify-center" : ""}`}
      >
        {iconPill(active)}
        {!collapsed && <span className="flex-1 truncate">{item.label}</span>}
        {!collapsed && !!item.badge && item.badge > 0 && <NavBadge count={item.badge} tone="destructive" />}
        {collapsed && !!item.badge && item.badge > 0 && (
          <span className="absolute right-0.5 top-0.5 grid h-4 min-w-[1rem] place-items-center rounded-full bg-destructive px-1 text-[10px] font-semibold leading-none text-destructive-foreground">
            {item.badge > 99 ? "99+" : item.badge}
          </span>
        )}
      </Link>
    </li>
  );
};

/**
 * Decorative animated network/dot pattern shown at the bottom of the
 * sidebar. Uses currentColor + opacity so it adapts to both themes
 * (light/dark) without hardcoded hex values.
 */
const SidebarBackdrop = ({ collapsed }: { collapsed: boolean }) => (
  <div
    aria-hidden
    className="pointer-events-none absolute inset-x-0 bottom-0 z-0 h-56 overflow-hidden opacity-60 dark:opacity-70"
  >
    <svg
      viewBox="0 0 240 220"
      preserveAspectRatio="xMidYMax slice"
      className="absolute inset-0 h-full w-full text-primary"
    >
      <defs>
        <radialGradient id="sb-glow" cx="50%" cy="100%" r="80%">
          <stop offset="0%" stopColor="currentColor" stopOpacity="0.18" />
          <stop offset="100%" stopColor="currentColor" stopOpacity="0" />
        </radialGradient>
        <pattern id="sb-dots" x="0" y="0" width="14" height="14" patternUnits="userSpaceOnUse">
          <circle cx="1.5" cy="1.5" r="1.1" fill="currentColor" opacity="0.55" />
        </pattern>
      </defs>
      <rect x="0" y="0" width="240" height="220" fill="url(#sb-glow)" />
      <rect x="0" y="40" width="240" height="180" fill="url(#sb-dots)" opacity="0.35">
        <animate attributeName="opacity" values="0.2;0.5;0.2" dur="4s" repeatCount="indefinite" />
      </rect>
      {/* Curva animada estilo "rede / onda" */}
      <path
        d="M -10 180 Q 60 110 130 150 T 260 90"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.2"
        strokeLinecap="round"
        opacity="0.7"
      >
        <animate attributeName="d"
          values="M -10 180 Q 60 110 130 150 T 260 90;
                  M -10 170 Q 60 140 130 130 T 260 110;
                  M -10 180 Q 60 110 130 150 T 260 90"
          dur="6s" repeatCount="indefinite" />
      </path>
      <path
        d="M -10 200 Q 80 150 160 175 T 260 140"
        fill="none"
        stroke="currentColor"
        strokeWidth="0.8"
        strokeLinecap="round"
        opacity="0.45"
      >
        <animate attributeName="d"
          values="M -10 200 Q 80 150 160 175 T 260 140;
                  M -10 195 Q 80 175 160 160 T 260 160;
                  M -10 200 Q 80 150 160 175 T 260 140"
          dur="7s" repeatCount="indefinite" />
      </path>
      {/* Pontos pulsando */}
      {[
        { cx: 40, cy: 170, d: "2s" },
        { cx: 110, cy: 140, d: "3s" },
        { cx: 180, cy: 165, d: "2.5s" },
        { cx: 220, cy: 120, d: "3.5s" },
      ].map((p, i) => (
        <circle key={i} cx={p.cx} cy={p.cy} r="2" fill="currentColor">
          <animate attributeName="opacity" values="0.2;1;0.2" dur={p.d} repeatCount="indefinite" />
          <animate attributeName="r" values="1.5;3;1.5" dur={p.d} repeatCount="indefinite" />
        </circle>
      ))}
    </svg>
    {collapsed && <div className="absolute inset-0" />}
  </div>
);

const NavBadge = ({ count, tone }: { count: number; tone: "destructive" | "emerald" }) => {
  const cls =
    tone === "destructive"
      ? "bg-destructive text-destructive-foreground"
      : "bg-emerald-500 text-white";
  return (
    <span className={`grid h-4 min-w-[1rem] place-items-center rounded-full px-1 text-[10px] font-semibold leading-none ${cls}`}>
      {count > 99 ? "99+" : count}
    </span>
  );
};
