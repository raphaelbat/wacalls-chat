import { Check, Globe } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SUPPORTED_LANGS, changeLanguage, type LangCode } from "@/i18n";

const FlagIcon = ({ country, className }: { country: string; className?: string }) => (
  <img
    src={`https://flagcdn.com/w40/${country}.png`}
    srcSet={`https://flagcdn.com/w80/${country}.png 2x`}
    alt=""
    aria-hidden
    className={className ?? "h-4 w-6 rounded-[2px] object-cover shadow-sm"}
  />
);

/**
 * Compact flag + ISO code language picker for the header.
 * Persists choice to localStorage and best-effort syncs to user profile.
 */
export const LanguageSwitcher = () => {
  const { t, i18n } = useTranslation();
  const current = SUPPORTED_LANGS.find((l) => i18n.language?.startsWith(l.code.split("-")[0])) ?? SUPPORTED_LANGS[0];

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label={t("language.change")}
          className="flex h-9 items-center gap-1.5 rounded-full border border-border/60 bg-muted/40 px-2.5 text-xs font-semibold text-foreground transition hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
        >
          <FlagIcon country={current.country} />
          <span className="tracking-wide">{current.short}</span>
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-52">
        <DropdownMenuLabel className="flex items-center gap-2 text-xs">
          <Globe className="h-3.5 w-3.5" /> {t("language.label")}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {SUPPORTED_LANGS.map((lng) => {
          const active = current.code === lng.code;
          return (
            <DropdownMenuItem
              key={lng.code}
              onSelect={() => void changeLanguage(lng.code as LangCode)}
              className="gap-2"
            >
              <FlagIcon country={lng.country} />
              <span className="flex-1">{lng.label}</span>
              {active && <Check className="h-4 w-4 text-primary" />}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
};