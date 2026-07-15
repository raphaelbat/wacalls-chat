import { useNavigate } from "react-router-dom";
import { ChevronDown, Code2, LogOut, Moon, ShieldCheck, Sun, User as UserIcon } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useAuth } from "@/stores/auth";
import { apiUrl } from "@/lib/api-base";
import { useTheme } from "@/stores/theme";
import { useTranslation } from "react-i18next";

// initialsFor renders 1–2 letters from the user's email/name as a fallback
// avatar so the header always shows a recognizable identity mark.
const initialsFor = (label: string): string => {
  const cleaned = label.trim();
  if (!cleaned) return "?";
  const parts = cleaned.split(/[\s@.]+/).filter(Boolean);
  if (parts.length === 0) return cleaned.slice(0, 1).toUpperCase();
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[1][0]).toUpperCase();
};

export const UserMenu = () => {
  const { t } = useTranslation();
  const user = useAuth((s) => s.user);
  const doLogout = useAuth((s) => s.logout);
  const nav = useNavigate();
  const theme = useTheme((s) => s.theme);
  const toggleTheme = useTheme((s) => s.toggle);
  if (!user) return null;
  const isAdmin = user.roles?.includes("admin");
  const label = user.companyName || user.email;
  const initials = initialsFor(label);
  const avatarSrc = user.avatarUrl
    ? /^https?:\/\//i.test(user.avatarUrl)
      ? user.avatarUrl
      : apiUrl(user.avatarUrl)
    : null;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label={t("header.account")}
          className="group flex h-9 items-center gap-2 rounded-full border border-border/60 bg-muted/40 pl-1 pr-2 text-left transition hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 sm:pr-3"
        >
          <span className="grid h-7 w-7 shrink-0 place-items-center overflow-hidden rounded-full bg-primary/10 text-[11px] font-semibold text-primary">
            {avatarSrc ? (
              <img src={avatarSrc} alt="" className="h-full w-full object-cover" />
            ) : (
              initials
            )}
          </span>
          <span className="hidden min-w-0 flex-col leading-tight sm:flex">
            <span className="truncate text-[12px] font-semibold text-foreground">{label}</span>
            <span className="truncate text-[10px] text-muted-foreground">{user.email}</span>
          </span>
          <ChevronDown className="hidden h-3.5 w-3.5 text-muted-foreground transition group-data-[state=open]:rotate-180 sm:block" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        <DropdownMenuLabel className="flex items-start gap-3 py-3">
          <span className="grid h-10 w-10 shrink-0 place-items-center overflow-hidden rounded-full bg-primary/10 text-sm font-semibold text-primary">
            {avatarSrc ? (
              <img src={avatarSrc} alt="" className="h-full w-full object-cover" />
            ) : (
              initials
            )}
          </span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm font-medium">{label}</span>
            <span className="block truncate text-xs font-normal text-muted-foreground">{user.email}</span>
            {isAdmin && (
              <span className="mt-1 inline-flex items-center gap-1 rounded-full bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-amber-500">
                <ShieldCheck className="h-3 w-3" /> {t("userMenu.admin")}
              </span>
            )}
          </span>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          className="gap-2"
          onSelect={(e) => {
            e.preventDefault();
            toggleTheme();
          }}
        >
          {theme === "dark" ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
          {theme === "dark" ? t("userMenu.lightMode") : t("userMenu.darkMode")}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          className="gap-2 text-destructive focus:text-destructive"
          onSelect={async () => {
            await doLogout();
            nav("/login", { replace: true });
          }}
        >
          <LogOut className="h-4 w-4" /> {t("userMenu.logout")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
};