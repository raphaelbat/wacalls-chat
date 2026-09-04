import * as settingsApi from "@/services/settings";

const WL_EVENT = "whitelabel:changed";
const WL_CACHE_KEY = "wl:cache:v1";

function hexToHslTriplet(hex: string): string | null {
  const m = /^#?([0-9a-fA-F]{6}|[0-9a-fA-F]{3})$/.exec(hex.trim());
  if (!m) return null;
  let h = m[1];
  if (h.length === 3) h = h.split("").map((c) => c + c).join("");
  const r = parseInt(h.slice(0, 2), 16) / 255;
  const g = parseInt(h.slice(2, 4), 16) / 255;
  const b = parseInt(h.slice(4, 6), 16) / 255;
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const l = (max + min) / 2;
  let s = 0;
  let hh = 0;
  if (max !== min) {
    const d = max - min;
    s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
    switch (max) {
      case r: hh = (g - b) / d + (g < b ? 6 : 0); break;
      case g: hh = (b - r) / d + 2; break;
      default: hh = (r - g) / d + 4;
    }
    hh /= 6;
  }
  return `${Math.round(hh * 360)} ${Math.round(s * 100)}% ${Math.round(l * 100)}%`;
}

/** Luminância relativa (WCAG) de uma cor hex, 0 (preto) a 1 (branco). */
function relativeLuminance(hex: string): number | null {
  const m = /^#?([0-9a-fA-F]{6}|[0-9a-fA-F]{3})$/.exec(hex.trim());
  if (!m) return null;
  let h = m[1];
  if (h.length === 3) h = h.split("").map((c) => c + c).join("");
  const r = parseInt(h.slice(0, 2), 16) / 255;
  const g = parseInt(h.slice(2, 4), 16) / 255;
  const b = parseInt(h.slice(4, 6), 16) / 255;
  const toLinear = (c: number) => (c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4));
  return 0.2126 * toLinear(r) + 0.7152 * toLinear(g) + 0.0722 * toLinear(b);
}

/**
 * Escolhe preto ou branco para `--primary-foreground` com base na
 * luminância relativa (WCAG) da cor primária escolhida. Sem isso, uma cor
 * primária clara (ex.: branco) deixa o texto dos botões/badges invisível,
 * já que o foreground ficava fixo em branco no CSS base.
 */
function idealForegroundHsl(hex: string): string {
  const luminance = relativeLuminance(hex);
  if (luminance === null) return "0 0% 100%";
  // Fundo claro -> texto quase preto; fundo escuro -> texto branco.
  return luminance > 0.5 ? "0 0% 9%" : "0 0% 100%";
}

/** Mesma lógica de `idealForegroundHsl`, mas devolvendo um hex — útil para
 * `style={{ color }}` em mockups/prévias fora do sistema de CSS vars. */
export function idealForegroundHex(hex: string): "#171717" | "#ffffff" {
  const luminance = relativeLuminance(hex);
  if (luminance === null) return "#ffffff";
  return luminance > 0.5 ? "#171717" : "#ffffff";
}

function setFavicon(url?: string) {
  if (!url) return;
  // Remove any existing icon links to prevent the browser from briefly
  // showing the default/static favicon alongside the whitelabel one.
  const existing = document.querySelectorAll<HTMLLinkElement>(
    "link[rel~='icon'], link[rel='shortcut icon'], link[rel='apple-touch-icon']",
  );
  existing.forEach((l) => l.parentElement?.removeChild(l));

  // Regular favicon
  const link = document.createElement("link");
  link.rel = "icon";
  link.href = url;
  document.head.appendChild(link);

  // Apple touch icon (app icon for home screen)
  const appleLink = document.createElement("link");
  appleLink.rel = "apple-touch-icon";
  appleLink.href = url;
  document.head.appendChild(appleLink);
}

/** Updates (or creates) a `<meta name|property="...">` tag in <head>. */
function setMeta(attr: "name" | "property", key: string, content: string) {
  let el = document.head.querySelector<HTMLMetaElement>(`meta[${attr}="${key}"]`);
  if (!el) {
    el = document.createElement("meta");
    el.setAttribute(attr, key);
    document.head.appendChild(el);
  }
  el.setAttribute("content", content);
}

/**
 * Mirrors the whitelabel brand name into every place a crawler / browser
 * tab can pick it up: <title>, meta description, Open Graph and Twitter
 * cards. Keeps a generic product tagline that adapts to the brand name.
 */
function applyBrandMeta(appName: string, favicon?: string) {
  const name = appName.trim();
  if (!name) return;
  const tagline = `${name} — Atendimento, chamadas e WhatsApp em uma única plataforma`;
  const description = `${name} unifica atendimento, ligações VoIP e WhatsApp em uma plataforma multiusuário, com filas, campanhas, URA e relatórios em tempo real.`;
  // O título da aba mostra exatamente o nome cadastrado no Whitelabel — sem
  // sufixo adicional — para não exibir texto que o administrador não digitou.
  document.title = name;
  setMeta("name", "description", description);
  setMeta("property", "og:site_name", name);
  setMeta("property", "og:title", tagline);
  setMeta("property", "og:description", description);
  setMeta("name", "twitter:title", tagline);
  setMeta("name", "twitter:description", description);
  if (favicon) {
    // Reuse the whitelabel favicon as the social/OG preview image so that
    // shared links (WhatsApp, Telegram, etc.) reflect the current brand.
    setMeta("property", "og:image", favicon);
    setMeta("name", "twitter:image", favicon);
    setMeta("property", "og:image:alt", name);
  }
}

function writeCache(wl: settingsApi.Whitelabel) {
  try {
    localStorage.setItem(
      WL_CACHE_KEY,
      JSON.stringify({
        appName: wl.appName,
        favicon: wl.favicon,
        primaryLight: wl.primaryLight,
        primaryDark: wl.primaryDark,
        iconColorLight: wl.iconColorLight,
        iconColorDark: wl.iconColorDark,
        logoLight: wl.logoLight,
        logoDark: wl.logoDark,
        splash: wl.splash,
      }),
    );
  } catch {
    /* ignore quota */
  }
}

export function readCachedWhitelabel(): Partial<settingsApi.Whitelabel> | null {
  try {
    const raw = localStorage.getItem(WL_CACHE_KEY);
    return raw ? (JSON.parse(raw) as Partial<settingsApi.Whitelabel>) : null;
  } catch {
    return null;
  }
}

export function applyCachedWhitelabel() {
  const wl = readCachedWhitelabel();
  if (wl) applyWhitelabel(wl as settingsApi.Whitelabel);
}

export function applyWhitelabel(wl: settingsApi.Whitelabel) {
  const isDark = document.documentElement.classList.contains("dark");
  const primary = (isDark ? wl.primaryDark : wl.primaryLight) || wl.primaryLight || wl.primaryDark;
  if (primary) {
    const hsl = hexToHslTriplet(primary);
    if (hsl) {
      document.documentElement.style.setProperty("--primary", hsl);
      document.documentElement.style.setProperty("--ring", hsl);
      // Recalcula o texto sobre a cor primária (preto ou branco) para que
      // cores claras (ex.: branco) não deixem o texto dos botões invisível.
      document.documentElement.style.setProperty("--primary-foreground", idealForegroundHsl(primary));
    }
  } else {
    // Sem cor customizada (ex.: após "Restaurar Padrões") — remove os
    // overrides inline para os valores padrão do CSS voltarem a valer.
    document.documentElement.style.removeProperty("--primary");
    document.documentElement.style.removeProperty("--ring");
    document.documentElement.style.removeProperty("--primary-foreground");
  }

  // Cor dos ícones do menu — campo independente da cor primária. Se não
  // for definida, --nav-icon volta a seguir --primary (ver styles/index.css).
  const iconColor = (isDark ? wl.iconColorDark : wl.iconColorLight) || wl.iconColorLight || wl.iconColorDark;
  if (iconColor) {
    const iconHsl = hexToHslTriplet(iconColor);
    if (iconHsl) document.documentElement.style.setProperty("--nav-icon", iconHsl);
  } else {
    document.documentElement.style.removeProperty("--nav-icon");
  }

  if (wl.appName) applyBrandMeta(wl.appName, wl.favicon);
  setFavicon(wl.favicon);
  writeCache(wl);
}

export async function loadAndApplyWhitelabel() {
  try {
    const wl = await settingsApi.getWhitelabel();
    applyWhitelabel(wl);
  } catch {
    /* ignore — whitelabel é opcional */
  }
}

export function emitWhitelabelChanged(wl: settingsApi.Whitelabel) {
  applyWhitelabel(wl);
  window.dispatchEvent(new CustomEvent(WL_EVENT, { detail: wl }));
}

export function subscribeWhitelabel(cb: (wl: settingsApi.Whitelabel) => void) {
  const handler = (e: Event) => cb((e as CustomEvent).detail as settingsApi.Whitelabel);
  window.addEventListener(WL_EVENT, handler);
  return () => window.removeEventListener(WL_EVENT, handler);
}