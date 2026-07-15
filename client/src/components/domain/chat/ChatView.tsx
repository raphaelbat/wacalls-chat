import { useEffect, useMemo, useRef, useState } from "react";
import { CheckCheck, History, KanbanSquare, Mic, Paperclip, Phone, PhoneOff, Send, Smile, UserPlus, Image as ImageIcon, FileText, Film, Contact2, Signature, StickyNote, Workflow, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useChats, setChatStatus } from "@/stores/chats";
import { useAuth } from "@/stores/auth";
import { assignChat, closeChat, deleteMessage, editMessage, forwardMessage, getSignature, listChatClosures, listChatEvents, resolveLidPhone, sendContact, sendMedia, sendMessage, sendNote, setSignature as saveSignature, triggerFlow } from "@/services/chats";
import { listContacts } from "@/services/contacts";
import { useOptionsStore } from "@/stores/options";
import { listUsers } from "@/services/auth";
import { listFlows } from "@/services/flows";
import type { FlowRow } from "@/types/flow";
import { MessageBubble } from "./MessageBubble";
import { ForwardDialog } from "./ForwardDialog";
import { KanbanLinkDialog } from "./KanbanLinkDialog";
import { EmojiPicker } from "./EmojiPicker";
import { ContactDetailsPanel } from "./ContactDetailsPanel";
import { ChatTagsManager } from "./ChatTagsManager";
import type { Tag } from "@/types/tag";
import { listChatTags } from "@/services/tags";
import { tagChipStyle } from "@/lib/tag-color";
import { formatPhone } from "@/lib/phone-format";
import { formatPeer, formatDayHeader, isGroupJid } from "./format";
import type { ChatClosure, ChatEvent, ChatMessage, ChatSummary } from "@/types/chat";
import { eventStream } from "@/lib/event-stream";
import { useDevices } from "@/stores/devices";
import { useStartCall } from "@/hooks/useStartCall";
import { useEndCall } from "@/hooks/useEndCall";
import { useCalls } from "@/stores/calls";
import { useSessions } from "@/stores/sessions";
import { listQueues } from "@/services/queues";
import type { Queue } from "@/types/queue";
import { rememberMessage, suggestMessages } from "@/lib/message-suggestions";
import { cardsByChat, listBoards, getBoard } from "@/services/kanban";
import type { KanbanBoard, KanbanCard, KanbanColumn } from "@/types/kanban";

const EMPTY_MESSAGES: ChatMessage[] = [];

interface Props {
  sessionId: string;
  chatJid: string | null;
  onStatusChange?: (status: "open" | "waiting" | "closed") => void;
}

export const ChatView = ({ sessionId, chatJid, onStatusChange }: Props) => {
  const myId = useAuth((s) => s.user?.id ?? null);
  const myUser = useAuth((s) => s.user);
  const messages = useChats((s) =>
    chatJid ? s.messagesBySession[sessionId]?.[chatJid] ?? EMPTY_MESSAGES : EMPTY_MESSAGES,
  );
  const chats = useChats((s) => s.chatsBySession[sessionId]);
  const chat = useMemo<ChatSummary | undefined>(
    () => chats?.find((c) => c.chatJid === chatJid),
    [chats, chatJid],
  );

  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);
  const [showEmoji, setShowEmoji] = useState(false);
  const [showAttach, setShowAttach] = useState(false);
  const [showContact, setShowContact] = useState(false);
  const [signature, setSignatureState] = useState<{ enabled: boolean; text: string }>({ enabled: false, text: "" });
  const [showSignatureEditor, setShowSignatureEditor] = useState(false);
  const [closeReason, setCloseReason] = useState("");
  const [closing, setClosing] = useState(false);
  const [showHistory, setShowHistory] = useState(false);
  const [closures, setClosures] = useState<ChatClosure[]>([]);
  const [events, setEvents] = useState<ChatEvent[]>([]);
  const [userMap, setUserMap] = useState<Record<string, string>>({});
  const [loadingHistory, setLoadingHistory] = useState(false);
  // Real phone number resolved from a WhatsApp LID. The local-part of a
  // "<digits>@lid" JID is NOT a phone number — showing it raw would render
  // garbage like "+110101620904079". We ask the backend to translate it.
  const [lidPhone, setLidPhone] = useState<string | null>(null);

  useEffect(() => {
    setLidPhone(null);
    if (!sessionId || !chatJid || !chatJid.endsWith("@lid")) return;
    let cancelled = false;
    void resolveLidPhone(sessionId, chatJid).then((r) => {
      if (!cancelled && r?.phone) setLidPhone(r.phone);
    });
    return () => {
      cancelled = true;
    };
  }, [sessionId, chatJid]);
  const [forwardTarget, setForwardTarget] = useState<ChatMessage | null>(null);
  const [editing, setEditing] = useState<ChatMessage | null>(null);
  const [editingText, setEditingText] = useState("");
  const [replyTo, setReplyTo] = useState<ChatMessage | null>(null);
  const [noteMode, setNoteMode] = useState(false);
  const [showFlows, setShowFlows] = useState(false);
  const [suggestIdx, setSuggestIdx] = useState(-1);
  const [showSuggest, setShowSuggest] = useState(true);

  const suggestions = useMemo(() => (showSuggest ? suggestMessages(text) : []), [text, showSuggest]);
  useEffect(() => { setSuggestIdx(-1); }, [text]);
  useEffect(() => { setShowSuggest(true); }, [chatJid]);
  const [flows, setFlows] = useState<FlowRow[]>([]);
  const [loadingFlows, setLoadingFlows] = useState(false);
  const [showKanban, setShowKanban] = useState(false);
  const [showContactDetails, setShowContactDetails] = useState(false);
  const [chatTags, setChatTags] = useState<Tag[]>([]);
  // Kanban cards linked to this conversation. Rendered as small chips in
  // the header so the operator instantly sees which board/column tracks
  // this atendimento.
  type KanbanChip = { card: KanbanCard; board?: KanbanBoard; column?: KanbanColumn };
  const [kanbanChips, setKanbanChips] = useState<KanbanChip[]>([]);
  // Chat lifecycle timeline (created / opened / closed / requeued / transferred).
  // Rendered as system pills merged into the message list by timestamp so the
  // operator sees the same audit trail WhatsApp Business shows on tickets.
  const [chatEvents, setChatEvents] = useState<ChatEvent[]>([]);

  // Queue (fila) configured on the WhatsApp connection. Rendered as a small
  // colored chip beside the contact name so the operator instantly sees which
  // team/fila this conversation belongs to.
  const sessionQueueId = useSessions((s) => s.sessions.find((x) => x.id === sessionId)?.queueId);
  const [queues, setQueues] = useState<Queue[]>([]);
  useEffect(() => {
    let cancelled = false;
    listQueues()
      .then((rows) => {
        if (!cancelled) setQueues(rows);
      })
      .catch(() => {
        if (!cancelled) setQueues([]);
      });
    return () => {
      cancelled = true;
    };
  }, []);
  const sessionQueue = useMemo(
    () => (sessionQueueId ? queues.find((q) => q.id === sessionQueueId) : undefined),
    [queues, sessionQueueId],
  );

  useEffect(() => {
    if (!sessionId || !chatJid) {
      setChatTags([]);
      return;
    }
    let cancelled = false;
    listChatTags(sessionId, chatJid)
      .then((rows) => {
        if (!cancelled) setChatTags(rows);
      })
      .catch(() => {
        if (!cancelled) setChatTags([]);
      });
    return () => {
      cancelled = true;
    };
  }, [sessionId, chatJid]);

  // Load Kanban cards linked to this chat plus their board/column so we can
  // render a chip in the header. Also refreshes when the link dialog closes.
  useEffect(() => {
    if (!sessionId || !chatJid || showKanban) return;
    let cancelled = false;
    (async () => {
      try {
        const cards = await cardsByChat(sessionId, chatJid);
        if (cancelled) return;
        if (cards.length === 0) {
          setKanbanChips([]);
          return;
        }
        const boards = await listBoards().catch(() => [] as KanbanBoard[]);
        const boardsById = new Map(boards.map((b) => [b.id, b]));
        const columnCache = new Map<string, KanbanColumn[]>();
        const chips: KanbanChip[] = [];
        for (const card of cards) {
          let cols = columnCache.get(card.boardId);
          if (!cols) {
            try {
              const snap = await getBoard(card.boardId);
              cols = snap.columns;
            } catch {
              cols = [];
            }
            columnCache.set(card.boardId, cols);
          }
          chips.push({
            card,
            board: boardsById.get(card.boardId),
            column: cols.find((c) => c.id === card.columnId),
          });
        }
        if (!cancelled) setKanbanChips(chips);
      } catch {
        if (!cancelled) setKanbanChips([]);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [sessionId, chatJid, showKanban]);

  // Load the lifecycle timeline whenever the active conversation changes and
  // subscribe to live "chat-event" SSE pushes so newly logged actions appear
  // without refetching.
  useEffect(() => {
    if (!sessionId || !chatJid) {
      setChatEvents([]);
      return;
    }
    let cancelled = false;
    listChatEvents(sessionId, chatJid)
      .then((rows) => {
        if (!cancelled) setChatEvents(rows);
      })
      .catch(() => {
        if (!cancelled) setChatEvents([]);
      });
    const off = eventStream.on((ev) => {
      if (ev.type !== "chat-event") return;
      const e = ev.event;
      if (e.sessionId !== sessionId || e.chatJid !== chatJid) return;
      setChatEvents((prev) => (prev.some((p) => p.id === e.id) ? prev : [...prev, e]));
    });
    return () => {
      cancelled = true;
      off();
    };
  }, [sessionId, chatJid]);

  const scrollRef = useRef<HTMLDivElement>(null);
  const requireCloseReason = useOptionsStore((s) => !!s.options.requireCloseReason);
  const imgInputRef = useRef<HTMLInputElement>(null);
  const videoInputRef = useRef<HTMLInputElement>(null);
  const docInputRef = useRef<HTMLInputElement>(null);
  const messageInputRef = useRef<HTMLInputElement>(null);

  // Audio recording
  const recorderRef = useRef<MediaRecorder | null>(null);
  const recordChunksRef = useRef<Blob[]>([]);
  const [recording, setRecording] = useState(false);
  const [recordSecs, setRecordSecs] = useState(0);
  const recordTimerRef = useRef<number | null>(null);
  const recordStreamRef = useRef<MediaStream | null>(null);
  const audioCtxRef = useRef<AudioContext | null>(null);
  const analyserRef = useRef<AnalyserNode | null>(null);
  const rafRef = useRef<number | null>(null);
  const waveCanvasRef = useRef<HTMLCanvasElement | null>(null);

  // Signature comes from the user profile (/api/me/signature). The per-conversation
  // toggle (localStorage) only overrides the "enabled" flag for this chat; the
  // text is always the one saved in the profile so the agent doesn't have to
  // retype it for every conversation.
  const sigKey = chatJid ? `voipinho.sig.${sessionId}:${chatJid}` : null;
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const remote = await getSignature();
        let enabled = !!remote.enabled;
        if (sigKey) {
          const raw = localStorage.getItem(sigKey);
          if (raw) {
            try {
              const parsed = JSON.parse(raw) as { enabled?: boolean };
              if (typeof parsed.enabled === "boolean") enabled = parsed.enabled;
            } catch {
              // ignore
            }
          }
        }
        if (!cancelled) setSignatureState({ enabled, text: remote.text ?? "" });
      } catch {
        if (!cancelled) setSignatureState({ enabled: false, text: "" });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [sigKey]);
  const persistSignature = (next: { enabled: boolean; text: string }) => {
    setSignatureState(next);
    if (sigKey) {
      try {
        localStorage.setItem(sigKey, JSON.stringify({ enabled: next.enabled }));
      } catch {
        // ignore quota
      }
    }
    // Persist text changes back to the profile so it survives across chats.
    void saveSignature(next.enabled, next.text).catch(() => undefined);
  };

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight });
  }, [messages.length, chatJid]);

  useEffect(() => () => {
    // Ensure no MediaRecorder / mic stream leaks across navigation.
    // Without this, the browser tab keeps showing the "recording" indicator
    // after the user switches pages while a voice note is being captured.
    try { recorderRef.current?.stop(); } catch { /* ignore */ }
    recorderRef.current = null;
    try { recordStreamRef.current?.getTracks().forEach((t) => t.stop()); } catch { /* ignore */ }
    recordStreamRef.current = null;
    teardownAudioVis();
  }, []);

  if (!chatJid) {
    return (
      <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
        Selecione uma conversa para ver as mensagens.
      </div>
    );
  }

  const displayName = chat?.name && chat.name.trim() !== "" ? chat.name : formatPeer(chatJid);
  const status = chat?.status ?? "open";
  const isGroup = !!chat?.isGroup || isGroupJid(chatJid);
  const canSend = isGroup || status === "open";

  const handleSend = async () => {
    const value = text.trim();
    if (!value || sending) return;
    // Private note path: never hits WhatsApp, only stored as kind="note".
    // Allowed even when the chat is still in "waiting" (no agent assigned).
    if (noteMode) {
      setSending(true);
      try {
        await sendNote(sessionId, chatJid!, value);
          rememberMessage(value);
        setText("");
        setReplyTo(null);
      } catch (e) {
        console.error("note failed", e);
      } finally {
        setSending(false);
      // Mantém o foco no campo de digitação após enviar (Enter não deve sair do input).
      requestAnimationFrame(() => messageInputRef.current?.focus());
      }
      return;
    }
    if (!canSend) return;
    let composed = value;
    if (replyTo) {
      const quoted = quotePreview(replyTo);
      composed = `${quoted}\n${value}`;
    }
    // Assinatura automática: o nome cadastrado do usuário tem precedência
    // sobre qualquer texto antigo salvo no perfil. Assim, sempre que o
    // atendente tiver um nome no cadastro, ele aparecerá como assinatura
    // ("*Raphael*") sem precisar reconfigurar nada.
    const autoName = (myUser?.name?.trim()
      || myUser?.companyName?.trim()
      || (myUser?.email ? myUser.email.split("@")[0] : "")
      || "").trim();
    let sigText = autoName || signature.text.trim();
    if (signature.enabled && !sigText) sigText = signature.text.trim();
    const finalText = signature.enabled && sigText ? `*${sigText}*\n${composed}` : composed;
    setSending(true);
    try {
      await sendMessage(sessionId, chatJid, finalText);
      rememberMessage(value);
      setText("");
      setShowEmoji(false);
      setReplyTo(null);
    } catch (e) {
      console.error("send failed", e);
    } finally {
      setSending(false);
      requestAnimationFrame(() => messageInputRef.current?.focus());
    }
  };

  const handleFile = async (file: File | null, kind: "image" | "video" | "audio" | "document") => {
    if (!file) return;
    if (!canSend) return;
    setShowAttach(false);
    setSending(true);
    try {
      await sendMedia(sessionId, chatJid, file, kind, text.trim());
      setText("");
    } catch (e) {
      console.error("media send failed", e);
    } finally {
      setSending(false);
    }
  };

  const startRecording = async () => {
    if (!canSend) return;
    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
      });
      const mime = pickAudioMime();
      const rec = mime ? new MediaRecorder(stream, { mimeType: mime }) : new MediaRecorder(stream);
      recordStreamRef.current = stream;
      recordChunksRef.current = [];
      rec.ondataavailable = (ev) => {
        if (ev.data.size > 0) recordChunksRef.current.push(ev.data);
      };
      rec.onstop = async () => {
        stream.getTracks().forEach((t) => t.stop());
        teardownAudioVis();
        const type = rec.mimeType || "audio/webm";
        const blob = new Blob(recordChunksRef.current, { type });
        if (blob.size < 200) {
          console.warn("audio too short to send");
          return;
        }
        const ext = type.includes("ogg") ? "ogg" : type.includes("mp4") ? "m4a" : "webm";
        const file = new File([blob], `audio-${Date.now()}.${ext}`, { type });
        try {
          setSending(true);
          await sendMedia(sessionId, chatJid, file, "audio");
        } catch (e) {
          console.error("audio send failed", e);
        } finally {
          setSending(false);
        }
      };
      recorderRef.current = rec;
      rec.start(250);
      setRecording(true);
      setRecordSecs(0);
      recordTimerRef.current = window.setInterval(() => setRecordSecs((n) => n + 1), 1000);
      // Audio visualization
      try {
        const AC = (window as unknown as { AudioContext?: typeof AudioContext; webkitAudioContext?: typeof AudioContext }).AudioContext
          || (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
        if (AC) {
          const ctx = new AC();
          const src = ctx.createMediaStreamSource(stream);
          const analyser = ctx.createAnalyser();
          analyser.fftSize = 256;
          src.connect(analyser);
          audioCtxRef.current = ctx;
          analyserRef.current = analyser;
          drawWaveform();
        }
      } catch (e) {
        console.warn("waveform unavailable", e);
      }
    } catch (e) {
      console.error("mic error", e);
      const err = e as { name?: string };
      if (err?.name === "NotAllowedError") {
        alert("Permissão de microfone negada. Habilite no navegador para gravar áudio.");
      } else if (err?.name === "NotFoundError") {
        alert("Nenhum microfone encontrado.");
      } else if (err?.name === "NotReadableError") {
        alert("O microfone está em uso por outro aplicativo.");
      }
    }
  };

  const stopRecording = () => {
    try { recorderRef.current?.stop(); } catch { /* ignore */ }
    recorderRef.current = null;
    // Always release the mic tracks so the browser tab stops showing the
    // red "recording" indicator. The MediaRecorder.stop() call alone does
    // NOT close the underlying MediaStream tracks.
    try { recordStreamRef.current?.getTracks().forEach((t) => t.stop()); } catch { /* ignore */ }
    recordStreamRef.current = null;
    teardownAudioVis();
    setRecording(false);
  };

  const cancelRecording = () => {
    try {
      recorderRef.current?.removeEventListener("stop", () => {});
    } catch { /* ignore */ }
    try { recorderRef.current?.stop(); } catch { /* ignore */ }
    recordChunksRef.current = [];
    recorderRef.current = null;
    recordStreamRef.current?.getTracks().forEach((t) => t.stop());
    recordStreamRef.current = null;
    teardownAudioVis();
    setRecording(false);
  };

  const teardownAudioVis = () => {
    if (rafRef.current != null) { cancelAnimationFrame(rafRef.current); rafRef.current = null; }
    if (recordTimerRef.current != null) { window.clearInterval(recordTimerRef.current); recordTimerRef.current = null; }
    if (audioCtxRef.current) { try { void audioCtxRef.current.close(); } catch { /* ignore */ } audioCtxRef.current = null; }
    analyserRef.current = null;
  };

  const drawWaveform = () => {
    const analyser = analyserRef.current;
    const canvas = waveCanvasRef.current;
    if (!analyser || !canvas) {
      rafRef.current = requestAnimationFrame(drawWaveform);
      return;
    }
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const w = canvas.width;
    const h = canvas.height;
    const bins = analyser.frequencyBinCount;
    const buf = new Uint8Array(bins);
    analyser.getByteFrequencyData(buf);
    ctx.clearRect(0, 0, w, h);
    const bars = 28;
    const step = Math.floor(bins / bars);
    const bw = (w / bars) - 2;
    ctx.fillStyle = "rgb(16, 185, 129)";
    for (let i = 0; i < bars; i++) {
      let sum = 0;
      for (let j = 0; j < step; j++) sum += buf[i * step + j] ?? 0;
      const v = sum / step / 255;
      const bh = Math.max(2, v * h);
      ctx.fillRect(i * (bw + 2), (h - bh) / 2, bw, bh);
    }
    rafRef.current = requestAnimationFrame(drawWaveform);
  };

  const toggleSignature = () => persistSignature({ ...signature, enabled: !signature.enabled });

  const openFlowPicker = async () => {
    setShowFlows((v) => !v);
    if (flows.length === 0 && !loadingFlows) {
      setLoadingFlows(true);
      try {
        const all = await listFlows();
        // O atendimento dispara apenas fluxos de conversa (chatbot);
        // fluxos de voz (URA) só rodam em chamadas telefônicas.
        setFlows(all.filter((f: any) => (f?.kind ?? "chat") !== "voice"));
      } catch (e) {
        console.error("list flows failed", e);
      } finally {
        setLoadingFlows(false);
      }
    }
  };

  const fireFlow = async (flowId: string) => {
    if (!chatJid) return;
    setShowFlows(false);
    try {
      await triggerFlow(sessionId, chatJid, flowId);
    } catch (e) {
      console.error("trigger flow failed", e);
    }
  };

  const confirmClose = async () => {
    setClosing(true);
    try {
      await closeChat(sessionId, chatJid, closeReason.trim());
      setChatStatus(sessionId, chatJid, "closed", null);
      onStatusChange?.("closed");
      setCloseReason("");
      // Refresh history if it's open so the new entry shows up.
      if (showHistory) {
        try {
          const [cls, evs] = await Promise.all([
            listChatClosures(sessionId, chatJid),
            listChatEvents(sessionId, chatJid),
          ]);
          setClosures(cls);
          setEvents(evs);
        } catch {
          // ignore
        }
      }
    } catch (e) {
      console.error("close chat failed", e);
    } finally {
      setClosing(false);
    }
  };

  const openHistory = async () => {
    setShowHistory(true);
    setLoadingHistory(true);
    try {
      const [cls, evs, users] = await Promise.all([
        listChatClosures(sessionId, chatJid),
        listChatEvents(sessionId, chatJid),
        listUsers().catch(() => []),
      ]);
      setClosures(cls);
      setEvents(evs);
      const map: Record<string, string> = {};
      for (const u of users) {
        if (u?.id) map[u.id] = u.name || u.email || u.id;
      }
      setUserMap(map);
    } catch (e) {
      console.error("load history failed", e);
      setClosures([]);
      setEvents([]);
    } finally {
      setLoadingHistory(false);
    }
  };

  let lastDay = "";
  // Build a single timeline merging chat messages and lifecycle events sorted
  // chronologically. Each entry remembers its original kind so the renderer
  // chooses between MessageBubble (chats) and a system pill (events).
  type TimelineItem =
    | { kind: "msg"; ts: number; key: string; msg: ChatMessage }
    | { kind: "evt"; ts: number; key: string; evt: ChatEvent };
  // Group reaction messages onto their target so each emoji renders as a
  // chip under the original bubble instead of a separate "Reação — 😊"
  // entry that hides which message was reacted to.
  //
  // Reactions can arrive two ways:
  //   1) Structured: kind === "reaction" with quotedId pointing at the target.
  //   2) Legacy text: a normal message whose body is "Reação — 👍" (or with
  //      a leading 🙂). Older backends emit this when whatsmeow surfaces a
  //      ReactionMessage as plain text without a quoted id. We salvage these
  //      by anchoring the emoji to the most recent prior message from the
  //      OPPOSITE party (the message being reacted to) within the same chat.
  const reactionsByTarget = new Map<string, Array<{ emoji: string; fromMe: boolean; senderName?: string }>>();
  const hiddenReactionIds = new Set<string>();
  // Pattern: optional leading emoji + "Reação"/"Reaction" + dash + emoji.
  const REACTION_RE = /^\s*(?:[\p{Extended_Pictographic}\u2600-\u27BF]\s*)?(?:Reação|Reaction|Reacted)\s*[—\-:]\s*(\S.*?)\s*$/u;
  // Pre-sort by ts so "most recent prior" lookups are correct regardless of
  // insertion order in the source list.
  const sortedForReactions = [...messages].sort((a, b) => a.ts - b.ts);
  // Build an id set so we can verify a quotedId actually resolves to a
  // visible bubble. If the referenced message isn't loaded (paginated out,
  // or id mismatch between backends) we fall back to the nearest prior
  // message so the emoji never floats alone in the timeline.
  const normalizedIdToRealId = new Map<string, string>();
  for (const m of sortedForReactions) {
    for (const key of normalizeMessageIdCandidates(m.id)) normalizedIdToRealId.set(key, m.id);
  }
  const resolveTargetId = (quotedId?: string): string | undefined => {
    if (!quotedId) return undefined;
    for (const key of normalizeMessageIdCandidates(quotedId)) {
      const found = normalizedIdToRealId.get(key);
      if (found) return found;
    }
    return undefined;
  };
  const actorKeyFor = (m: ChatMessage): string => (m.fromMe ? "__me__" : (m.senderName || "__them__"));
  const reactionActorKey = (r: { fromMe: boolean; senderName?: string }): string => r.fromMe ? "__me__" : (r.senderName || "__them__");
  const hasSameReactionOnTarget = (targetId: string, actorKey: string, emoji: string): boolean => {
    const list = reactionsByTarget.get(targetId) || [];
    return list.some((r) => r.emoji === emoji && reactionActorKey(r) === actorKey);
  };
  // For the fallback path (no quotedId), each actor's reactions must spread
  // across DIFFERENT messages — otherwise 3 emojis from the same actor all
  // collapse onto the same most-recent opposite-party bubble.
  const actorAlreadyReactedTo = (targetId: string, actorKey: string): boolean => {
    const list = reactionsByTarget.get(targetId) || [];
    return list.some((r) => reactionActorKey(r) === actorKey);
  };
  const findFallbackTarget = (i: number, fromMe: boolean, actorKey: string): string | undefined => {
    // Prefer the most recent prior message from the OPPOSITE party
    // (the message being reacted to), skipping ones this actor already
    // reacted to so multiple emojis spread across distinct bubbles.
    for (let j = i - 1; j >= 0; j--) {
      const prev = sortedForReactions[j];
      if (prev.kind === "reaction") continue;
      if (prev.kind === "text" && prev.body && REACTION_RE.test(prev.body)) continue;
      if (actorAlreadyReactedTo(prev.id, actorKey)) continue;
      if (prev.fromMe !== fromMe) return prev.id;
    }
    // Otherwise, fall back to the immediately previous non-reaction message
    // (group chats, self-reactions, etc.).
    for (let j = i - 1; j >= 0; j--) {
      const prev = sortedForReactions[j];
      if (prev.kind === "reaction") continue;
      if (prev.kind === "text" && prev.body && REACTION_RE.test(prev.body)) continue;
      if (actorAlreadyReactedTo(prev.id, actorKey)) continue;
      return prev.id;
    }
    return undefined;
  };
  for (let i = 0; i < sortedForReactions.length; i++) {
    const m = sortedForReactions[i];
    let targetId: string | undefined;
    let emoji: string | undefined;
    const actorKey = actorKeyFor(m);
    if (m.kind === "reaction" && m.body) {
      emoji = m.body;
      targetId = resolveTargetId(m.quotedId);
    } else if (m.kind === "text" && m.body && REACTION_RE.test(m.body)) {
      const match = m.body.match(REACTION_RE);
      emoji = match?.[1]?.trim();
      targetId = resolveTargetId(m.quotedId);
    } else {
      continue;
    }
    if (!emoji) continue;
    // If the quotedId doesn't resolve to a visible message, treat it as
    // missing and fall back to a timeline-based anchor. This keeps the
    // emoji visually attached to a bubble instead of getting dropped
    // (when the quoted message hasn't been loaded yet) or hidden.
    if (!targetId || hasSameReactionOnTarget(targetId, actorKey, emoji)) {
      targetId = findFallbackTarget(i, m.fromMe, actorKey);
    }
    if (!targetId) continue;
    const list = reactionsByTarget.get(targetId) || [];
    // De-dupe identical emoji from the same actor (WhatsApp resends
    // reaction events when the user re-taps the same emoji).
    if (!list.some((r) => r.emoji === emoji && reactionActorKey(r) === actorKey)) {
      list.push({ emoji, fromMe: m.fromMe, senderName: m.senderName });
    }
    reactionsByTarget.set(targetId, list);
    hiddenReactionIds.add(m.id);
  }
  const timeline: TimelineItem[] = [
    ...messages
      // Hide any message we've already attached as a reaction chip — both
      // structured reactions and legacy "Reação — X" text rows.
      .filter((m) => !hiddenReactionIds.has(m.id))
      .map<TimelineItem>((m) => ({ kind: "msg", ts: m.ts, key: `m:${m.id}`, msg: m })),
    ...chatEvents.map<TimelineItem>((e) => ({ kind: "evt", ts: e.ts, key: `e:${e.id}`, evt: e })),
  ].sort((a, b) => a.ts - b.ts);
  return (
    <div className="relative flex flex-1 flex-col">
      <header className="flex items-center gap-3 border-b px-4 py-3">
        <button
          type="button"
          onClick={() => setShowContactDetails(true)}
          title="Ver dados do contato"
          className="relative grid h-9 w-9 shrink-0 place-items-center overflow-hidden rounded-full bg-primary/10 text-sm font-semibold text-primary ring-offset-background transition hover:ring-2 hover:ring-primary/40 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary"
        >
          {chat?.avatarUrl ? (
            <img
              src={chat.avatarUrl}
              alt={displayName}
              className="h-full w-full object-cover"
              loading="lazy"
              onError={(e) => {
                (e.currentTarget as HTMLImageElement).style.display = "none";
              }}
            />
          ) : (
            displayName.slice(0, 1).toUpperCase()
          )}
        </button>
        <button
          type="button"
          onClick={() => setShowContactDetails(true)}
          className="min-w-0 flex-1 text-left transition hover:opacity-80"
          title="Ver dados do contato"
        >
          <div className="flex items-center gap-1.5">
            <span className="truncate text-sm font-semibold">{displayName}</span>
            {sessionQueue && (
              <span
                className="inline-flex max-w-[160px] shrink-0 items-center truncate rounded-full px-2 py-0.5 text-[10px] font-semibold leading-none"
                style={tagChipStyle(sessionQueue.color)}
                title={`Fila da conexão: ${sessionQueue.name}`}
              >
                {sessionQueue.name}
              </span>
            )}
            {kanbanChips.map((chip) => {
              const color = chip.column?.color || chip.board?.color || "#6366f1";
              const label = chip.column?.name || chip.board?.name || "Kanban";
              const full = `${chip.board?.name ?? "Kanban"} · ${chip.column?.name ?? ""} · ${chip.card.title}`;
              return (
                <span
                  key={chip.card.id}
                  className="inline-flex max-w-[180px] shrink-0 items-center gap-1 truncate rounded-full px-2 py-0.5 text-[10px] font-semibold leading-none"
                  style={tagChipStyle(color)}
                  title={full}
                >
                  <KanbanSquare className="h-2.5 w-2.5" />
                  {label}
                </span>
              );
            })}
          </div>
          <div className="flex items-center gap-1.5">
            <span className="truncate text-[11px] text-muted-foreground">
              {isGroup
                ? "Grupo"
                : chatJid.endsWith("@lid")
                  ? (lidPhone ? formatPhone(lidPhone) : "Número oculto")
                  : formatPhone(chatJid)}
            </span>
          </div>
        </button>
        <div className="flex items-center gap-2">
          {!isGroup && (
            <CallButtons sessionId={sessionId} chatJid={chatJid} lidPhone={lidPhone} />
          )}
          <Button
            size="sm"
            variant="ghost"
            title="Logs do atendimento"
            onClick={() => (showHistory ? setShowHistory(false) : void openHistory())}
          >
            <History className="h-4 w-4" />
          </Button>
          <Button
            size="sm"
            variant="ghost"
            title="Vincular ao Kanban"
            onClick={() => setShowKanban(true)}
          >
            <KanbanSquare className="h-4 w-4" />
          </Button>
          {!isGroup && status === "waiting" && (
            <>
              <Button
                size="sm"
                variant="default"
                onClick={async () => {
                  try {
                    await assignChat(sessionId, chatJid);
                    setChatStatus(sessionId, chatJid, "open", myId);
                    onStatusChange?.("open");
                  } catch (e) {
                    console.error("assign chat failed", e);
                  }
                }}
              >
                <UserPlus className="mr-1 h-3.5 w-3.5" /> Atender
              </Button>
              <Button size="sm" variant="outline" onClick={() => void confirmClose()} disabled={closing}>
                <CheckCheck className="mr-1 h-3.5 w-3.5" /> Finalizar
              </Button>
            </>
          )}
          {!isGroup && status === "open" && (
            <Button size="sm" variant="outline" onClick={() => void confirmClose()} disabled={closing}>
              <CheckCheck className="mr-1 h-3.5 w-3.5" /> Finalizar
            </Button>
          )}
          {isGroup && status !== "closed" && (
            <Button size="sm" variant="outline" onClick={() => void confirmClose()} disabled={closing}>
              <CheckCheck className="mr-1 h-3.5 w-3.5" /> Finalizar
            </Button>
          )}
        </div>
      </header>
      <div ref={scrollRef} className="chat-doodle-bg flex-1 overflow-y-auto px-4 py-3">
        {timeline.length === 0 ? (
          <div className="grid h-full place-items-center text-xs text-muted-foreground">Sem mensagens ainda.</div>
        ) : (
          <ul className="flex flex-col gap-1">
            {timeline.map((item) => {
              if (item.kind === "evt") return null;
              const day = formatDayHeader(item.ts);
              const showDay = day !== lastDay;
              lastDay = day;
              const m = item.msg;
              return (
                <li key={item.key} className="flex flex-col">
                  {showDay && (
                    <div className="my-2 self-center rounded-full bg-background px-2 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">
                      {day}
                    </div>
                  )}
                  <MessageBubble
                    message={m}
                    showSender={isGroup && !m.fromMe}
                    reactions={reactionsByTarget.get(m.id)}
                    onForward={(msg) => setForwardTarget(msg)}
                    onReply={(msg) => setReplyTo(msg)}
                    onEdit={(msg) => {
                      setEditing(msg);
                      setEditingText(msg.body);
                    }}
                    onDelete={async (msg) => {
                      if (!chatJid) return;
                      if (!window.confirm("Apagar esta mensagem para todos?")) return;
                      try {
                        await deleteMessage(sessionId, chatJid, msg.id);
                      } catch (e) {
                        console.error("delete failed", e);
                        alert("Não foi possível apagar (mensagens antigas podem não permitir).");
                      }
                    }}
                  />
                </li>
              );
            })}
          </ul>
        )}
      </div>
      {showSignatureEditor && (
        <div className="border-t bg-card px-3 py-2 text-xs">
          <div className="mb-1 flex items-center justify-between">
            <span className="font-medium">Assinatura do atendente (esta conversa)</span>
            <button className="text-muted-foreground hover:text-foreground" onClick={() => setShowSignatureEditor(false)}>fechar</button>
          </div>
          <input
            value={signature.text}
            onChange={(e) => persistSignature({ ...signature, text: e.target.value })}
            placeholder="Ex.: Rafael"
            className="w-full rounded-md border bg-background px-2 py-1 outline-none focus:ring-1 focus:ring-ring"
          />
          <div className="mt-1 text-[10px] text-muted-foreground">
            Salvo automaticamente. Estado da assinatura é por conversa.
          </div>
        </div>
      )}
      {showContact && (
        <ContactForm
          onCancel={() => setShowContact(false)}
          onSubmit={async (name, phone) => {
            setShowContact(false);
            setSending(true);
            try {
              await sendContact(sessionId, chatJid, name, phone);
            } finally {
              setSending(false);
            }
          }}
        />
      )}
      {showEmoji && (
        <div className="border-t bg-background p-2">
          <EmojiPicker onPick={(e) => setText((t) => t + e)} />
        </div>
      )}
      {replyTo && (
        <div className="flex items-start gap-2 border-t bg-muted/40 px-3 py-2 text-xs">
          <div className="w-1 self-stretch rounded-full bg-primary" />
          <div className="min-w-0 flex-1">
            <div className="font-medium text-primary">
              Respondendo a {replyTo.fromMe ? "você" : (replyTo.senderName || "contato")}
            </div>
            <div className="truncate text-muted-foreground">
              {replyPreviewText(replyTo)}
            </div>
          </div>
          <button
            type="button"
            onClick={() => setReplyTo(null)}
            className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
            aria-label="Cancelar resposta"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      )}
      {!isGroup && status === "waiting" && (
        <div className="border-t border-amber-400/40 bg-amber-100/40 px-3 py-2 text-center text-xs text-amber-700 dark:bg-amber-500/10 dark:text-amber-300">
          Este atendimento está <strong>aguardando</strong>. Aceite o ticket para poder enviar mensagens.
        </div>
      )}
      <form
        className="flex items-center gap-2 border-t bg-background px-3 py-2"
        onSubmit={(e) => {
          e.preventDefault();
          void handleSend();
        }}
      >
        {(() => { return null; })()}
        <button
          type="button"
          title={noteMode ? "Mensagem privada ATIVA (clique para desativar)" : "Mensagem privada (nota interna)"}
          onClick={() => setNoteMode((v) => !v)}
          className={`rounded-md p-2 ${noteMode ? "bg-amber-400/20 text-amber-600 dark:text-amber-300" : "text-muted-foreground hover:bg-muted hover:text-foreground"}`}
        >
          <StickyNote className="h-4 w-4" />
        </button>
        <button
          type="button"
          title="Emojis"
          onClick={() => setShowEmoji((v) => !v)}
          disabled={!canSend}
          className="rounded-md p-2 text-muted-foreground hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent disabled:hover:text-muted-foreground"
        >
          <Smile className="h-4 w-4" />
        </button>
        <button
          type="button"
          title="Enviar contato"
          onClick={() => { setShowAttach(false); setShowContact((v) => !v); }}
          disabled={!canSend}
          className={`rounded-md p-2 disabled:cursor-not-allowed disabled:opacity-40 ${showContact ? "bg-primary/15 text-primary" : "text-muted-foreground hover:bg-muted hover:text-foreground"}`}
        >
          <Contact2 className="h-4 w-4" />
        </button>

        <div className="relative">
          <button
            type="button"
            title="Anexar"
            onClick={() => setShowAttach((v) => !v)}
            disabled={!canSend}
            className="rounded-md p-2 text-muted-foreground hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent disabled:hover:text-muted-foreground"
          >
            <Paperclip className="h-4 w-4" />
          </button>
          {showAttach && (
            <div className="absolute bottom-full left-0 mb-2 w-44 overflow-hidden rounded-lg border bg-popover shadow-md">
              <AttachItem icon={<ImageIcon className="h-4 w-4" />} label="Imagem" onClick={() => imgInputRef.current?.click()} />
              <AttachItem icon={<Film className="h-4 w-4" />} label="Vídeo" onClick={() => videoInputRef.current?.click()} />
              <AttachItem icon={<FileText className="h-4 w-4" />} label="Documento" onClick={() => docInputRef.current?.click()} />
              <AttachItem icon={<Contact2 className="h-4 w-4" />} label="Contato" onClick={() => { setShowAttach(false); setShowContact(true); }} />
            </div>
          )}
        </div>
        <input ref={imgInputRef} type="file" accept="image/*" className="hidden" onChange={(e) => handleFile(e.target.files?.[0] ?? null, "image")} />
        <input ref={videoInputRef} type="file" accept="video/*" className="hidden" onChange={(e) => handleFile(e.target.files?.[0] ?? null, "video")} />
        <input ref={docInputRef} type="file" className="hidden" onChange={(e) => handleFile(e.target.files?.[0] ?? null, "document")} />

        <button
          type="button"
          title={signature.enabled ? "Assinatura ON (clique para editar)" : "Assinatura OFF"}
          onClick={(e) => {
            if (e.shiftKey) {
              setShowSignatureEditor((v) => !v);
            } else {
              void toggleSignature();
            }
          }}
          onContextMenu={(e) => { e.preventDefault(); setShowSignatureEditor((v) => !v); }}
          disabled={!canSend}
          className={`rounded-md p-2 disabled:cursor-not-allowed disabled:opacity-40 ${signature.enabled ? "bg-primary/15 text-primary" : "text-muted-foreground hover:bg-muted hover:text-foreground"}`}
        >
          <Signature className="h-4 w-4" />
        </button>


        {recording ? (
          <div className="flex flex-1 items-center gap-2 rounded-md border bg-background px-3 py-1.5">
            <span className="inline-block h-2 w-2 animate-pulse rounded-full bg-red-500" />
            <canvas ref={waveCanvasRef} width={320} height={28} className="h-7 flex-1" />
            <span className="font-mono text-xs tabular-nums text-muted-foreground">
              {String(Math.floor(recordSecs / 60)).padStart(2, "0")}:{String(recordSecs % 60).padStart(2, "0")}
            </span>
            <Button type="button" size="icon" variant="ghost" onClick={cancelRecording} aria-label="Cancelar gravação" title="Cancelar">
              <X className="h-4 w-4" />
            </Button>
            <Button type="button" size="icon" onClick={stopRecording} aria-label="Enviar áudio" title="Enviar">
              <Send className="h-4 w-4" />
            </Button>
          </div>
        ) : (
          <>
            <div className="relative flex-1">
              <input
                ref={messageInputRef}
                value={text}
                onChange={(e) => { setText(e.target.value); setShowSuggest(true); }}
                onPaste={(e) => {
                  const items = e.clipboardData?.items;
                  if (!items) return;
                  for (let i = 0; i < items.length; i++) {
                    const it = items[i];
                    if (it.kind === "file") {
                      const file = it.getAsFile();
                      if (!file) continue;
                      e.preventDefault();
                      const kind: "image" | "video" | "audio" | "document" =
                        file.type.startsWith("image/") ? "image" :
                        file.type.startsWith("video/") ? "video" :
                        file.type.startsWith("audio/") ? "audio" : "document";
                      void handleFile(file, kind);
                      return;
                    }
                  }
                }}
                onKeyDown={(e) => {
                  if (suggestions.length > 0) {
                    if (e.key === "ArrowDown") {
                      e.preventDefault();
                      setSuggestIdx((i) => Math.min(suggestions.length - 1, i + 1));
                      return;
                    }
                    if (e.key === "ArrowUp") {
                      e.preventDefault();
                      setSuggestIdx((i) => Math.max(-1, i - 1));
                      return;
                    }
                    if (e.key === "Tab" || (e.key === "Enter" && suggestIdx >= 0)) {
                      e.preventDefault();
                      const pick = suggestions[suggestIdx >= 0 ? suggestIdx : 0];
                      if (pick) { setText(pick); setShowSuggest(false); }
                      return;
                    }
                    if (e.key === "Escape") {
                      setShowSuggest(false);
                      return;
                    }
                  }
                }}
                placeholder={noteMode ? "Nota privada (visível só para a equipe)" : "Digite uma mensagem (Ctrl+V cola imagens/arquivos)"}
                className={`w-full rounded-md border px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring ${
                  noteMode ? "border-amber-400/50 bg-amber-100/40 dark:bg-amber-500/10" : "bg-background"
                }`}
                disabled={sending || (!noteMode && !canSend)}
              />
              {suggestions.length > 0 && (
                <div className="absolute bottom-full left-0 right-0 z-20 mb-1 overflow-hidden rounded-md border bg-popover text-popover-foreground shadow-lg">
                  <div className="border-b px-2 py-1 text-[10px] uppercase tracking-wide text-muted-foreground">
                    Sugestões · Tab para aceitar
                  </div>
                  {suggestions.map((s, i) => (
                    <button
                      key={s + i}
                      type="button"
                      onMouseDown={(ev) => { ev.preventDefault(); setText(s); setShowSuggest(false); }}
                      onMouseEnter={() => setSuggestIdx(i)}
                      className={`block w-full truncate px-3 py-2 text-left text-sm ${i === suggestIdx ? "bg-accent" : "hover:bg-accent/60"}`}
                    >
                      {s}
                    </button>
                  ))}
                </div>
              )}
            </div>
            {text.trim() ? (
              <Button type="submit" size="icon" disabled={sending} aria-label="Enviar">
                <Send className="h-4 w-4" />
              </Button>
            ) : (
              <Button type="button" size="icon" onClick={startRecording} aria-label="Gravar áudio" disabled={sending || !canSend}>
                <Mic className="h-4 w-4" />
              </Button>
            )}
          </>
        )}
      </form>
      {showHistory && (
        <aside className="absolute right-0 top-0 z-20 flex h-full w-80 flex-col border-l bg-card shadow-lg">
          <div className="flex items-center justify-between border-b px-3 py-2">
            <div className="flex items-center gap-2 text-sm font-semibold">
              <History className="h-4 w-4" /> Logs do atendimento
            </div>
            <button
              type="button"
              onClick={() => setShowHistory(false)}
              className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
              aria-label="Fechar"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
          <div className="flex-1 overflow-y-auto p-3 text-sm">
            {loadingHistory ? (
              <div className="text-xs text-muted-foreground">Carregando…</div>
            ) : (() => {
              const KIND_LABEL: Record<string, string> = {
                created: "Conversa criada",
                waiting: "Aguardando atendimento",
                opened: "Atendimento iniciado",
                closed: "Atendimento encerrado",
                requeued: "Devolvido à fila",
                transferred: "Transferido",
                call_incoming: "Chamada recebida",
                call_outgoing: "Chamada realizada",
                call_answered: "Chamada atendida",
                call_missed: "Chamada perdida",
                call_rejected: "Chamada rejeitada",
                call_canceled: "Chamada cancelada",
                call_no_answer: "Chamada não atendida",
                call_ended: "Chamada encerrada",
              };
              const displayUser = (id?: string, email?: string) => {
                if (id && userMap[id]) return userMap[id];
                if (email) return email.split("@")[0];
                return id || "Sistema";
              };
              // Build a unified, chronological log. Match each "closed" event
              // with the corresponding closure entry to surface its reason.
              const closureByTs = new Map<number, ChatClosure>();
              for (const c of closures) closureByTs.set(c.closedAt, c);
              type LogItem = {
                key: string;
                ts: number;
                title: string;
                userId?: string;
                userEmail?: string;
                detail?: string;
                reason?: string;
              };
              const items: LogItem[] = [];
              const matched = new Set<number>();
              for (const e of events) {
                const item: LogItem = {
                  key: `e-${e.id}`,
                  ts: e.ts,
                  title: KIND_LABEL[e.kind] || e.kind,
                  userId: e.userId,
                  userEmail: e.userEmail,
                  detail: e.detail,
                };
                if (e.kind === "closed") {
                  // closures are stored in seconds and events typically in ms;
                  // try both to find a match within a small window.
                  for (const c of closures) {
                    if (matched.has(c.id)) continue;
                    const diff = Math.abs(c.closedAt * 1000 - e.ts);
                    if (diff <= 5000) {
                      item.reason = c.reason;
                      matched.add(c.id);
                      break;
                    }
                  }
                }
                items.push(item);
              }
              // Surface closures that didn't match any event (legacy data).
              for (const c of closures) {
                if (matched.has(c.id)) continue;
                items.push({
                  key: `c-${c.id}`,
                  ts: c.closedAt * 1000,
                  title: "Atendimento encerrado",
                  userId: c.userId,
                  userEmail: c.userEmail,
                  reason: c.reason,
                });
              }
              items.sort((a, b) => b.ts - a.ts);
              if (items.length === 0) {
                return (
                  <div className="text-xs text-muted-foreground">
                    Nenhum evento registrado para esta conversa.
                  </div>
                );
              }
              return (
                <ul className="space-y-3">
                  {items.map((it) => (
                    <li key={it.key} className="rounded-md border bg-background p-2">
                      <div className="flex items-center justify-between gap-2 text-[11px] text-muted-foreground">
                        <span>{new Date(it.ts).toLocaleString()}</span>
                        <span className="truncate pl-2 font-medium text-foreground">
                          {displayUser(it.userId, it.userEmail)}
                        </span>
                      </div>
                      <div className="mt-1 text-xs font-semibold">{it.title}</div>
                      {it.detail && (
                        <div className="mt-0.5 text-[11px] text-muted-foreground">{it.detail}</div>
                      )}
                      {it.reason !== undefined && (
                        <div className="mt-1 whitespace-pre-wrap text-xs">
                          {it.reason?.trim() ? (
                            <>
                              <span className="text-muted-foreground">Motivo: </span>
                              {it.reason}
                            </>
                          ) : (
                            <span className="italic text-muted-foreground">sem motivo informado</span>
                          )}
                        </div>
                      )}
                    </li>
                  ))}
                </ul>
              );
            })()}
          </div>
        </aside>
      )}
      {editing && (
        <div className="fixed inset-0 z-50 grid place-items-center bg-black/50 p-4" onClick={() => setEditing(null)}>
          <div className="w-full max-w-md rounded-lg border bg-card p-4 shadow-xl" onClick={(e) => e.stopPropagation()}>
            <div className="mb-2 flex items-center justify-between">
              <div className="text-sm font-semibold">Editar mensagem</div>
              <button onClick={() => setEditing(null)} className="rounded-md p-1 text-muted-foreground hover:bg-muted">
                <X className="h-4 w-4" />
              </button>
            </div>
            <textarea
              value={editingText}
              onChange={(e) => setEditingText(e.target.value)}
              rows={3}
              className="w-full resize-none rounded-md border bg-background px-2 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
            />
            <div className="mt-1 text-[10px] text-muted-foreground">O WhatsApp só permite editar mensagens com até 15 minutos.</div>
            <div className="mt-3 flex justify-end gap-2">
              <Button size="sm" variant="ghost" onClick={() => setEditing(null)}>Cancelar</Button>
              <Button
                size="sm"
                disabled={!editingText.trim() || editingText.trim() === editing.body}
                onClick={async () => {
                  if (!chatJid || !editing) return;
                  try {
                    await editMessage(sessionId, chatJid, editing.id, editingText.trim());
                    setEditing(null);
                  } catch (e) {
                    console.error("edit failed", e);
                    alert("Não foi possível editar (talvez fora da janela de 15 minutos).");
                  }
                }}
              >
                Salvar
              </Button>
            </div>
          </div>
        </div>
      )}
      {forwardTarget && (
        <ForwardDialog
          sessionId={sessionId}
          currentJid={chatJid}
          message={forwardTarget}
          chats={chats ?? []}
          onClose={() => setForwardTarget(null)}
          onSubmit={async (targets) => {
            if (!chatJid || !forwardTarget) return;
            try {
              await forwardMessage(sessionId, chatJid, forwardTarget.id, targets);
              setForwardTarget(null);
            } catch (e) {
              console.error("forward failed", e);
              alert("Não foi possível encaminhar.");
            }
          }}
        />
      )}
      {chatJid && (
        <KanbanLinkDialog
          open={showKanban}
          onOpenChange={setShowKanban}
          sessionId={sessionId}
          chatJid={chatJid}
          defaultTitle={chat?.name || formatPeer(chatJid)}
        />
      )}
      {chatJid && (
        <ContactDetailsPanel
          open={showContactDetails}
          onOpenChange={setShowContactDetails}
          sessionId={sessionId}
          chatJid={chatJid}
          chat={chat}
          messages={messages}
          onTagsChange={setChatTags}
        />
      )}
    </div>
  );
};

const AttachItem = ({ icon, label, onClick }: { icon: React.ReactNode; label: string; onClick: () => void }) => (
  <button type="button" onClick={onClick} className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-muted">
    <span className="text-muted-foreground">{icon}</span>
    {label}
  </button>
);

// Picker that searches contacts already saved/seen on the user's WhatsApp
// sessions (no manual creation). The operator types a name/phone and picks
// from the list — exactly like the native "Enviar contato" picker.
const ContactForm = ({ onCancel, onSubmit }: { onCancel: () => void; onSubmit: (name: string, phone: string) => void | Promise<void> }) => {
  const [q, setQ] = useState("");
  const [results, setResults] = useState<Array<{ name: string; phone: string; chatJid: string; avatarUrl?: string }>>([]);
  const [loading, setLoading] = useState(false);
  useEffect(() => {
    let cancel = false;
    setLoading(true);
    const t = window.setTimeout(async () => {
      try {
        
        const r = await listContacts({ q: q.trim(), kind: "user", limit: 30 });
        if (cancel) return;
        // Dedup by phone since the same contact can exist across sessions.
        const seen = new Set<string>();
        const rows: typeof results = [];
        for (const c of r.contacts ?? []) {
          const phone = (c.phone || c.chatJid.split("@")[0] || "").replace(/[^\d+]/g, "");
          if (!phone || seen.has(phone)) continue;
          seen.add(phone);
          rows.push({ name: c.name || phone, phone, chatJid: c.chatJid, avatarUrl: c.avatarUrl });
        }
        setResults(rows);
      } catch {
        if (!cancel) setResults([]);
      } finally {
        if (!cancel) setLoading(false);
      }
    }, 220);
    return () => { cancel = true; window.clearTimeout(t); };
  }, [q]);
  return (
    <div className="border-t bg-card px-3 py-2 text-xs">
      <div className="mb-1 flex items-center justify-between">
        <span className="font-medium">Enviar contato</span>
        <button className="text-muted-foreground hover:text-foreground" onClick={onCancel}>fechar</button>
      </div>
      <input
        value={q}
        onChange={(e) => setQ(e.target.value)}
        placeholder="Buscar contato salvo (nome ou telefone)"
        className="w-full rounded-md border bg-background px-2 py-1 outline-none focus:ring-1 focus:ring-ring"
        autoFocus
      />
      <div className="mt-2 max-h-48 overflow-y-auto rounded-md border bg-background">
        {loading && <div className="px-2 py-2 text-muted-foreground">Buscando…</div>}
        {!loading && results.length === 0 && (
          <div className="px-2 py-2 text-muted-foreground">Nenhum contato encontrado.</div>
        )}
        {!loading && results.map((c) => (
          <button
            key={c.chatJid + c.phone}
            type="button"
            className="flex w-full items-center gap-2 px-2 py-1.5 text-left hover:bg-muted"
            onClick={() => onSubmit(c.name, c.phone)}
          >
            <div className="grid h-7 w-7 shrink-0 place-items-center overflow-hidden rounded-full bg-muted">
              {c.avatarUrl ? <img src={c.avatarUrl} alt="" className="h-full w-full object-cover" /> : <span className="text-[10px] uppercase">{(c.name || "?").slice(0, 2)}</span>}
            </div>
            <div className="min-w-0 flex-1">
              <div className="truncate font-medium">{c.name}</div>
              <div className="truncate text-[11px] text-muted-foreground">{c.phone}</div>
            </div>
          </button>
        ))}
      </div>
      <div className="mt-2 flex justify-end">
        <Button size="sm" variant="ghost" onClick={onCancel}>Cancelar</Button>
      </div>
    </div>
  );
};

function pickAudioMime(): string {
  const types = ["audio/ogg;codecs=opus", "audio/webm;codecs=opus", "audio/webm"];
  for (const t of types) if (typeof MediaRecorder !== "undefined" && MediaRecorder.isTypeSupported(t)) return t;
  return "";
}

// CallButtons surfaces a one-click audio/video call right from the chat
// header. For @lid conversations we must dial the resolved real phone
// number — dialing the LID itself fails because WhatsApp cannot route SRTP
// media to an anonymous LID address ("a chamada foi encerrada antes de
// iniciar"). If we haven't resolved the phone yet, the button stays
// disabled instead of triggering a doomed call.
const CallButtons = ({
  sessionId,
  chatJid,
  lidPhone,
}: {
  sessionId: string;
  chatJid: string;
  lidPhone: string | null;
}) => {
  const micId = useDevices((s) => s.micId);
  const outId = useDevices((s) => s.outId);
  const start = useStartCall(sessionId, micId, outId);
  const end = useEndCall();
  // Track active call for this chat. Match by sessionId and by the digits in
  // the peer JID/phone — peer may be stored as "+E164" or "<digits>@lid".
  const rawLocal = (chatJid.split("@")[0] ?? "").replace(/[^\d]/g, "");
  const server = chatJid.includes("@") ? chatJid.split("@")[1] : "";
  const realDigits =
    server === "lid"
      ? (lidPhone ?? "").replace(/[^\d]/g, "")
      : rawLocal;
  const activeCall = useCalls((s) =>
    s.calls.find((c) => {
      if (c.sessionId !== sessionId) return false;
      const peerDigits = (c.peer || "").replace(/[^\d]/g, "");
      return peerDigits && realDigits && peerDigits === realDigits;
    }),
  );
  // Only ever dial the real E.164 phone — never a LID. This mirrors the
  // Discador panel, which always sends "+digits".
  const target = realDigits ? `+${realDigits}` : "";
  const needsResolve = server === "lid" && !realDigits;
  const disabled = start.isPending || !target;
  if (activeCall) {
    const label =
      activeCall.status === "connected"
        ? "Em chamada"
        : activeCall.status === "ringing"
          ? "Chamando…"
          : "Conectando…";
    return (
      <div className="flex items-center gap-1.5">
        <span className="hidden items-center gap-1.5 rounded-full border border-emerald-500/30 bg-emerald-500/10 px-2 py-0.5 text-[11px] font-medium text-emerald-600 sm:inline-flex dark:text-emerald-400">
          <span className="relative flex h-1.5 w-1.5">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-500 opacity-75" />
            <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-emerald-500" />
          </span>
          {label}
        </span>
        <Button
          size="sm"
          variant="destructive"
          title="Desligar chamada"
          disabled={end.isPending}
          onClick={() => end.mutate({ sid: sessionId, callId: activeCall.callId })}
        >
          <PhoneOff className="h-4 w-4" />
        </Button>
      </div>
    );
  }
  return (
    <>
      <Button
        size="sm"
        variant="ghost"
        title={needsResolve ? "Número real não disponível para este contato" : "Ligar"}
        disabled={disabled}
        onClick={() => start.mutate({ phone: target, record: false, video: false })}
      >
        <Phone className="h-4 w-4 text-emerald-500" />
      </Button>
      {/* Vídeo chamada temporariamente desabilitada — será reativada após
          estabilizarmos a sinalização de vídeo do WhatsApp.
      <Button
        size="sm"
        variant="ghost"
        title="Vídeo chamada"
        disabled={disabled}
        onClick={() => start.mutate({ phone: target, record: false, video: true })}
      >
        <Video className="h-4 w-4 text-sky-500" />
      </Button>
      */}
    </>
  );
};
// Build a WhatsApp-style quoted prefix for outbound replies. Since the
// backend send endpoint accepts plain text, we inline the quoted preview as
// "> author: text" lines so the recipient sees the context.
function quotePreview(m: ChatMessage): string {
  const author = m.fromMe ? "Você" : (m.senderName?.trim() || "Contato");
  const raw = replyPreviewText(m).replace(/\s+/g, " ").trim();
  const truncated = raw.length > 140 ? raw.slice(0, 140) + "…" : raw;
  return `> *${author}:* ${truncated}`;
}

function replyPreviewText(m: ChatMessage): string {
  if (m.deleted) return "Mensagem apagada";
  if (m.kind === "image") return m.body?.trim() ? `📷 ${m.body.trim()}` : "📷 Imagem";
  if (m.kind === "video") return m.body?.trim() ? `🎥 ${m.body.trim()}` : "🎥 Vídeo";
  if (m.kind === "audio") return "🎵 Áudio";
  if (m.kind === "document") return `📎 ${m.fileName || "Documento"}`;
  if (m.kind === "sticker") return "🟦 Figurinha";
  return m.body || "";
}

function normalizeMessageIdCandidates(id: string | undefined): string[] {
  const raw = (id || "").trim();
  if (!raw) return [];
  const out = new Set<string>([raw]);
  const decoded = (() => {
    try {
      return decodeURIComponent(raw);
    } catch {
      return raw;
    }
  })();
  out.add(decoded);
  for (const value of [raw, decoded]) {
    const tail = value.split(/[/:|_\s]/).filter(Boolean).pop();
    if (tail) out.add(tail);
    for (const token of value.split(/[/:|_\s]/).filter((part) => part.length >= 8)) out.add(token);
    const hex = value.match(/[A-Fa-f0-9]{12,}/g);
    hex?.forEach((part) => out.add(part));
    const whatsAppMessageId = value.match(/(?:wamid\.|message:)?([A-Za-z0-9._-]{16,})$/);
    if (whatsAppMessageId?.[1]) out.add(whatsAppMessageId[1]);
  }
  return Array.from(out).filter(Boolean);
}

// EventPill renders a single lifecycle entry (created/opened/closed/...) as
// the centered amber "system message" that WhatsApp Business shows inside a
// conversation: short label + HH:MM stamp.
