// Base URL for the backend API.
// - In dev, leave VITE_API_BASE_URL unset and rely on Vite's proxy ("/api" -> :3001).
// - In production (separate backend host), set VITE_API_BASE_URL to e.g. "https://api.example.com".
// - When the Go server serves the SPA from the same origin, also leave it unset.
const raw = (import.meta.env.VITE_API_BASE_URL ?? "").toString().trim();

// Normalize: strip trailing slash so apiUrl("/api/x") never produces "//api/x".
export const API_BASE_URL = raw.replace(/\/+$/, "");

/** Resolve an API path against the configured base URL. */
export const apiUrl = (path: string): string => {
  if (/^https?:\/\//i.test(path)) return path;
  const p = path.startsWith("/") ? path : `/${path}`;
  return `${API_BASE_URL}${p}`;
};

/** True when the API lives on a different origin than the SPA. */
export const isCrossOrigin = (): boolean => {
  if (!API_BASE_URL) return false;
  try {
    return new URL(API_BASE_URL, window.location.href).origin !== window.location.origin;
  } catch {
    return false;
  }
};