export type FlowTrigger = "inbound" | "outbound" | "manual";

export interface FlowRow {
  id: string;
  name: string;
  trigger: FlowTrigger;
  graph: string;
  enabled: boolean;
  createdAt: number;
  updatedAt: number;
  /** Comma-separated keywords that trigger this flow on inbound messages. */
  keywords?: string;
  /** Match strategy used by the keyword router. */
  keywordMatch?: "any" | "exact" | "contains" | "starts_with";
}

export type FlowNodeType =
  | "voice_menu"
  | "message"
  | "whatsapp_send"
  | "whatsapp_media"
  | "record_audio"
  | "dtmf_capture"
  | "set_variable"
  | "ai_agent"
  | "webhook"
  | "end"
  | "condition"
  | "delay"
  | "transfer"
  // ===== Chat flow nodes =====
  // Conteúdo
  | "chat_content"
  | "chat_text"
  | "chat_media"
  | "chat_tag"
  // Interação
  | "chat_menu"
  | "chat_msg_api"
  | "chat_input"
  | "chat_interval"
  // Lógica
  | "chat_random"
  | "chat_if_else"
  // Sistema
  | "chat_queue"
  | "chat_tag_add"
  | "chat_tag_remove"
  | "chat_switch_flow"
  | "chat_attendant"
  // Integrações
  | "chat_http"
  | "chat_variable"
  | "chat_ai_agent"
  | "chat_n8n"
  // Instagram
  | "ig_trigger_comment"
  | "ig_reply_comment"
  | "ig_like_comment"
  | "ig_is_follower"
  | "ig_send_dm"
  | "ig_send_reward";

export interface FlowNodeData {
  // voice_menu
  prompt?: string;
  timeout?: number;
  // For voice_menu we also persist a per-option `next` (target node id)
  // so editing nodes wires up the option-keyed edges automatically.
  options?: Array<{ key: string; label: string; synonyms?: string[]; next?: string; type?: string; queueId?: string; queueName?: string }>;
  // voice_menu — silêncio que confirma o fim da fala do cliente.
  silenceMs?: number;
  // voice_menu — what to do on timeout/invalid speech. "" = re-toca menu.
  onTimeoutTarget?: string;
  onInvalidTarget?: string;
  // tts voice override (used by voice_menu, message). Maps to Piper voice
  // model name on the server, e.g. "pt_BR-faber-medium".
  voice?: string;
  // webhook
  url?: string;
  method?: "GET" | "POST" | "PUT" | "DELETE";
  body?: string;
  saveAs?: string;
  // condition
  variable?: string;
  operator?: "eq" | "neq" | "contains" | "starts_with" | "empty" | "not_empty" | "in";
  value?: string;
  // delay
  seconds?: number;
  // end
  mode?: "silent" | "spoken";
  // whatsapp_send
  template?: string;
  to?: string;
  // whatsapp_send — tipo de mensagem: text (padrão) ou botões interativos.
  msgKind?: "text" | "buttons";
  buttons?: Array<{ id?: string; text: string }>;
  // end — texto final opcional (TTS de despedida).
  finalText?: string;
  // ai_agent
  agentId?: string;
  context?: string;
  // transfer
  destination?: string;
  // record_audio
  // (uses `seconds` + `saveAs`)
  // whatsapp_media
  mediaKind?: "image" | "audio" | "video" | "document";
  mediaUrl?: string;
  caption?: string;
  filename?: string;
  // dtmf_capture
  maxDigits?: number;
  // set_variable
  // (uses `variable` + `value`)
  [k: string]: unknown;
}

export interface FlowNode {
  id: string;
  type: FlowNodeType;
  position: { x: number; y: number };
  data: FlowNodeData & { label?: string };
}

export interface FlowEdge {
  id: string;
  source: string;
  target: string;
  sourceHandle?: string;
}

export interface FlowGraph {
  nodes: FlowNode[];
  edges: FlowEdge[];
  startNodeId: string;
  /** Tipo do fluxo: voz (URA) ou conversa (chatbot). Default = "voice". */
  kind?: "voice" | "chat";
  voice?: {
    provider: "piper" | "elevenlabs" | "openai";
    voiceId: string;
    model?: string;
    elevenlabsApiKey?: string;
  };
}

export interface FlowRun {
  id: string;
  flowId: string;
  callId: string;
  sessionId: string;
  status: string;
  currentNode: string;
  variables: string;
  startedAt: number;
  endedAt?: number | null;
}

export interface FlowRunEvent {
  id: number;
  runId: string;
  ts: number;
  nodeId: string;
  eventType: string;
  payload: string;
}