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

function setFavicon(url?: string) {
  if (!url) return;
  // Remove any existing icon links to prevent the browser from briefly
  // showing the default/static favicon alongside the whitelabel one.
  const existing = document.querySelectorAll<HTMLLinkElement>(
    "link[rel~='icon'], link[rel='shortcut icon'], link[rel='apple-touch-icon']",
  );
  existing.forEach((l) => l.parentElement?.removeChild(l));
  const link = document.createElement("link");
  link.rel = "icon";
  link.href = url;
  document.head.appendChild(link);
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
  document.title = tagline;
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
        logoLight: wl.logoLight,
        logoDark: wl.logoDark,
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
    }
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