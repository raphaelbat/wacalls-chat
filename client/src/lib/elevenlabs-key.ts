// Persists the ElevenLabs API key once per workspace so flow/node dialogs
// don't keep asking for it. Stored in localStorage — never sent anywhere
// except the existing /api/tts/* endpoints.
const KEY = "voipinho.elevenlabs.apiKey";
const MODEL = "voipinho.elevenlabs.model";
const VOICE = "voipinho.elevenlabs.voiceId";

export function getSavedElevenLabsKey(): string {
  try {
    return localStorage.getItem(KEY) ?? "";
  } catch {
    return "";
  }
}

export function saveElevenLabsKey(key: string) {
  try {
    if (key) localStorage.setItem(KEY, key);
    else localStorage.removeItem(KEY);
  } catch {
    // ignore
  }
}

export function getSavedElevenLabsDefaults() {
  try {
    return {
      apiKey: localStorage.getItem(KEY) ?? "",
      model: localStorage.getItem(MODEL) ?? "",
      voiceId: localStorage.getItem(VOICE) ?? "",
    };
  } catch {
    return { apiKey: "", model: "", voiceId: "" };
  }
}

export function saveElevenLabsDefaults(d: { apiKey?: string; model?: string; voiceId?: string }) {
  try {
    if (d.apiKey !== undefined) {
      if (d.apiKey) localStorage.setItem(KEY, d.apiKey);
      else localStorage.removeItem(KEY);
    }
    if (d.model) localStorage.setItem(MODEL, d.model);
    if (d.voiceId) localStorage.setItem(VOICE, d.voiceId);
  } catch {
    // ignore
  }
}