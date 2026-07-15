import { create } from "zustand";

export type AIProvider =
  | "openai"
  | "anthropic"
  | "google"
  | "groq"
  | "mistral"
  | "deepseek"
  | "xai"
  | "perplexity"
  | "cohere"
  | "openrouter"
  ;

export type AIProviderInfo = {
  id: AIProvider;
  label: string;
  keyHint: string;
  docs: string;
  models: { id: string; label: string; recommended?: boolean }[];
};

export const AI_PROVIDERS: AIProviderInfo[] = [
  {
    id: "openai",
    label: "OpenAI",
    keyHint: "sk-...",
    docs: "https://platform.openai.com/api-keys",
    models: [
      { id: "gpt-4o-mini", label: "GPT-4o Mini", recommended: true },
      { id: "gpt-4o", label: "GPT-4o" },
      { id: "gpt-4.1-mini", label: "GPT-4.1 Mini" },
      { id: "gpt-4.1", label: "GPT-4.1" },
      { id: "o3-mini", label: "o3 Mini" },
    ],
  },
  {
    id: "anthropic",
    label: "Anthropic (Claude)",
    keyHint: "sk-ant-...",
    docs: "https://console.anthropic.com/settings/keys",
    models: [
      { id: "claude-3-5-sonnet-latest", label: "Claude 3.5 Sonnet", recommended: true },
      { id: "claude-3-5-haiku-latest", label: "Claude 3.5 Haiku" },
      { id: "claude-3-opus-latest", label: "Claude 3 Opus" },
    ],
  },
  {
    id: "google",
    label: "Google (Gemini)",
    keyHint: "AIza...",
    docs: "https://aistudio.google.com/app/apikey",
    models: [
      { id: "gemini-2.0-flash", label: "Gemini 2.0 Flash", recommended: true },
      { id: "gemini-1.5-pro", label: "Gemini 1.5 Pro" },
      { id: "gemini-1.5-flash", label: "Gemini 1.5 Flash" },
    ],
  },
  {
    id: "groq",
    label: "Groq",
    keyHint: "gsk_...",
    docs: "https://console.groq.com/keys",
    models: [
      { id: "llama-3.3-70b-versatile", label: "Llama 3.3 70B", recommended: true },
      { id: "llama-3.1-8b-instant", label: "Llama 3.1 8B Instant" },
      { id: "mixtral-8x7b-32768", label: "Mixtral 8x7B" },
    ],
  },
  {
    id: "mistral",
    label: "Mistral AI",
    keyHint: "...",
    docs: "https://console.mistral.ai/api-keys",
    models: [
      { id: "mistral-large-latest", label: "Mistral Large", recommended: true },
      { id: "mistral-small-latest", label: "Mistral Small" },
      { id: "open-mistral-nemo", label: "Mistral Nemo" },
    ],
  },
  {
    id: "deepseek",
    label: "DeepSeek",
    keyHint: "sk-...",
    docs: "https://platform.deepseek.com/api_keys",
    models: [
      { id: "deepseek-chat", label: "DeepSeek Chat", recommended: true },
      { id: "deepseek-reasoner", label: "DeepSeek Reasoner" },
    ],
  },
  {
    id: "xai",
    label: "xAI (Grok)",
    keyHint: "xai-...",
    docs: "https://console.x.ai/",
    models: [
      { id: "grok-2-latest", label: "Grok 2", recommended: true },
      { id: "grok-2-mini", label: "Grok 2 Mini" },
    ],
  },
  {
    id: "perplexity",
    label: "Perplexity",
    keyHint: "pplx-...",
    docs: "https://www.perplexity.ai/settings/api",
    models: [
      { id: "llama-3.1-sonar-large-128k-online", label: "Sonar Large", recommended: true },
      { id: "llama-3.1-sonar-small-128k-online", label: "Sonar Small" },
    ],
  },
  {
    id: "cohere",
    label: "Cohere",
    keyHint: "...",
    docs: "https://dashboard.cohere.com/api-keys",
    models: [
      { id: "command-r-plus", label: "Command R+", recommended: true },
      { id: "command-r", label: "Command R" },
    ],
  },
  {
    id: "openrouter",
    label: "OpenRouter",
    keyHint: "sk-or-...",
    docs: "https://openrouter.ai/keys",
    models: [
      { id: "openrouter/auto", label: "Auto (melhor disponível)", recommended: true },
      { id: "anthropic/claude-3.5-sonnet", label: "Claude 3.5 Sonnet" },
      { id: "openai/gpt-4o", label: "GPT-4o" },
    ],
  },
];

export const AI_LANGUAGES = [
  { code: "pt-BR", label: "Português (BR)", flag: "🇧🇷" },
  { code: "en", label: "English", flag: "🇺🇸" },
  { code: "es", label: "Español", flag: "🇪🇸" },
  { code: "fr", label: "Français", flag: "🇫🇷" },
  { code: "de", label: "Deutsch", flag: "🇩🇪" },
  { code: "it", label: "Italiano", flag: "🇮🇹" },
  { code: "auto", label: "Auto", flag: "🌐" },
];

export type AgentTone = "friendly" | "formal" | "casual" | "empathetic" | "direct" | "humorous";
export type AgentRole = "general" | "sales" | "support" | "scheduling" | "qualification" | "collections";

export type AIAgent = {
  id: string;
  name: string;
  // Básico
  role: AgentRole;
  tone: AgentTone;
  provider: AIProvider;
  model: string;
  apiKey?: string; // armazenada local
  languages: string[];
  personality?: string;
  active: boolean;
  // Comportamento
  firstMessage?: string;
  rules?: string;
  allowHandoff: boolean;
  handoffKeywords?: string;
  handoffMessage?: string;
  // Avançado
  responseDelay: number;     // segundos
  charLimit: number;         // máx caracteres
  humanize: boolean;
  audioEnabled: boolean;
  temperature: number;       // 0..2
  maxTokens: number;
  historySize: number;
  // Ferramentas
  shiftEnabled: boolean;
  inactivityEnabled: boolean;
  notifyHumanEnabled: boolean;
  httpToolEnabled: boolean;
  // Conhecimento
  knowledge: { id: string; label: string; kind: "text" | "url" | "file"; value: string }[];
  createdAt: number;
  updatedAt: number;
};

const STORAGE_KEY = "primevoip.agents.v1";

const load = (): AIAgent[] => {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? (parsed as AIAgent[]) : [];
  } catch {
    return [];
  }
};

const save = (items: AIAgent[]) => {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(items));
  } catch {
    /* noop */
  }
};

type State = {
  agents: AIAgent[];
  upsert: (a: Omit<AIAgent, "id" | "createdAt" | "updatedAt"> & { id?: string }) => AIAgent;
  remove: (id: string) => void;
};

export const useAgents = create<State>((set, get) => ({
  agents: load(),
  upsert: (input) => {
    const now = Date.now();
    const existing = input.id ? get().agents.find((a) => a.id === input.id) : undefined;
    const agent: AIAgent = {
      id: input.id ?? (crypto.randomUUID?.() ?? `a_${now}_${Math.random().toString(36).slice(2, 8)}`),
      ...input,
      name: input.name.trim(),
      apiKey: input.apiKey || existing?.apiKey,
      createdAt: existing?.createdAt ?? now,
      updatedAt: now,
    };
    const next = existing
      ? get().agents.map((a) => (a.id === agent.id ? agent : a))
      : [agent, ...get().agents];
    set({ agents: next });
    save(next);
    return agent;
  },
  remove: (id) => {
    const next = get().agents.filter((a) => a.id !== id);
    set({ agents: next });
    save(next);
  },
}));

/** Defaults para um novo agente. */
export const defaultAgent = (): Omit<AIAgent, "id" | "createdAt" | "updatedAt"> => ({
  name: "",
  role: "general",
  tone: "friendly",
  provider: "openai",
  model: "gpt-4o-mini",
  apiKey: "",
  languages: ["pt-BR"],
  personality: "",
  active: true,
  firstMessage: "Olá! Como posso ajudá-lo?",
  rules: "",
  allowHandoff: true,
  handoffKeywords: "atendente, sair, falar com humano, humano",
  handoffMessage: "Desculpe, não tenho informações sobre esse assunto. Vou transferir para um atendente.",
  responseDelay: 1,
  charLimit: 2000,
  humanize: true,
  audioEnabled: false,
  temperature: 1,
  maxTokens: 2000,
  historySize: 10,
  shiftEnabled: false,
  inactivityEnabled: false,
  notifyHumanEnabled: false,
  httpToolEnabled: false,
  knowledge: [],
});