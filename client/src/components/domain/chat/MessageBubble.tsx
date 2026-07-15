import { useEffect, useState, type ReactNode } from "react";
import { toast } from "sonner";
import {
  Ban,
  ChevronDown,
  Copy,
  CornerUpLeft,
  Download,
  ExternalLink,
  FileText,
  Forward,
  Pencil,
  Phone,
  Share2,
  Smile,
  Trash2,
  User as UserIcon,
} from "lucide-react";

import type { ChatMessage } from "@/types/chat";
import { formatTime, previewBody } from "./format";
import { AudioPlayer } from "./AudioPlayer";
import { ImageLightbox } from "./ImageLightbox";
import { listContacts } from "@/services/contacts";
import { resolveLidPhone } from "@/services/chats";
import { formatPhone } from "@/lib/phone-format";
import { setActiveChat } from "@/stores/chats";

const REACTION_EMOJIS = ["👍", "❤️", "😂", "😮", "😢", "🙏", "🔥", "👏"];
const DELETED_TEXTS = new Set([
  "mensagem",
  "esta mensagem foi apagada",
  "voce apagou esta mensagem",
  "você apagou esta mensagem",
  "this message was deleted",
  "you deleted this message",
]);

const isDeletedPlaceholder = (message: ChatMessage): boolean => {
  // Backends sometimes deliver revoked messages as kind="unknown" (label
  // "Mensagem") or as text with the localized placeholder. Treat both as
  // deleted so the bubble renders the "Esta mensagem foi apagada" badge
  // (and, when available, the locally snapshotted original content).
  const t = (message.body || "").trim().toLowerCase();
  if (message.kind === "text") return DELETED_TEXTS.has(t);
  if (message.kind === "unknown") return t === "" || DELETED_TEXTS.has(t);
  return false;
};

interface BubbleProps {
  message: ChatMessage;
  showSender?: boolean;
  onForward?: (m: ChatMessage) => void;
  onEdit?: (m: ChatMessage) => void;
  onDelete?: (m: ChatMessage) => void;
  onReply?: (m: ChatMessage) => void;
  // Reactions that target this message (already resolved via quotedId
  // by the parent ChatView). Rendered as a small chip under the bubble
  // so it's clear which message was reacted to.
  reactions?: Array<{ emoji: string; fromMe: boolean; senderName?: string }>;
}

export const MessageBubble = ({ message, showSender, onForward, onEdit, onDelete, onReply, reactions }: BubbleProps) => {
  const mine = message.fromMe;
  if (message.kind === "note") {
    return (
      <div className="flex justify-center">
        <div className="my-1 max-w-[85%] rounded-md border border-amber-400/50 bg-amber-100/80 px-3 py-1.5 text-xs text-amber-900 shadow-sm dark:bg-amber-500/15 dark:text-amber-200">
          <div className="mb-0.5 flex items-center justify-between gap-3 text-[10px] font-semibold uppercase tracking-wide opacity-80">
            <span>Nota privada{message.senderName ? ` · ${message.senderName}` : ""}</span>
            <span className="font-normal opacity-70">{formatTime(message.ts)}</span>
          </div>
          <div className="whitespace-pre-wrap break-words">{message.body}</div>
        </div>
      </div>
    );
  }
  const senderLabel = showSender ? senderDisplay(message) : "";
  const senderColor = senderLabel ? senderHue(message.senderJid) : "";
  const [menuOpen, setMenuOpen] = useState(false);
  const deleted = !!message.deleted || isDeletedPlaceholder(message);
  // When the contact (or operator) revokes a message we keep the previous
  // snapshot so the operator can still see the original content.
  const restoredBody = deleted ? (message.originalBody ?? "") : "";
  const restoredKind = deleted ? (message.originalKind ?? message.kind) : message.kind;
  const restoredMediaUrl = deleted ? message.originalMediaUrl : message.mediaUrl;
  const isMedia = !deleted && ["image", "video", "audio", "document", "sticker"].includes(message.kind);
  const caption = isMedia ? message.body : message.kind === "text" ? message.body : previewBody(message.kind, message.body);
  const body = deleted
    ? (restoredBody || (restoredMediaUrl ? "" : previewBody(restoredKind, "")))
    : caption;

  const showEdit = !!onEdit && mine && !deleted && message.kind === "text";
  const showDelete = !!onDelete && mine && !deleted;
  const showForward = !!onForward && !deleted;
  const showReply = !!onReply && !deleted;
  const mediaUrl = !deleted ? message.mediaUrl : undefined;
  const isDoc = message.kind === "document";
  const fileLabel = message.fileName || suggestFileName(message);
  const showCopy = !deleted && message.kind === "text" && !!message.body;
  const showOpen = !deleted && !!mediaUrl;
  const showDownload = !deleted && !!mediaUrl;
  const showShare = !deleted && (!!mediaUrl || !!message.body);
  const showReact = !deleted;
  const hasMenu =
    showEdit || showDelete || showForward || showReply || showCopy || showOpen || showDownload || showShare || showReact;
  const reactionChips = reactions?.length ? dedupeReactions(reactions) : [];

  const [reactOpen, setReactOpen] = useState(false);

  const closeAll = () => {
    setMenuOpen(false);
    setReactOpen(false);
  };

  const handleCopy = async () => {
    closeAll();
    try {
      await navigator.clipboard.writeText(message.body || "");
    } catch {
      /* ignore */
    }
  };

  const handleOpen = () => {
    closeAll();
    if (mediaUrl) window.open(mediaUrl, "_blank", "noopener,noreferrer");
  };

  const handleDownload = () => {
    closeAll();
    if (!mediaUrl) return;
    const a = document.createElement("a");
    a.href = mediaUrl;
    a.download = fileLabel;
    a.rel = "noopener";
    document.body.appendChild(a);
    a.click();
    a.remove();
  };

  const handleShare = async () => {
    closeAll();
    const url = mediaUrl ? new URL(mediaUrl, window.location.href).toString() : "";
    const text = message.body || "";
    // 1) Native Web Share when available (mobile / Safari / some Edge).
    if (typeof navigator !== "undefined" && typeof navigator.share === "function") {
      try {
        await navigator.share({ text, url: url || window.location.href });
        return;
      } catch (err) {
        if ((err as DOMException)?.name === "AbortError") return; // user cancelled
      }
    }
    // 2) Clipboard fallback so the user always gets a usable link/text.
    const payload = [text, url].filter(Boolean).join("\n").trim();
    try {
      await navigator.clipboard.writeText(payload || window.location.href);
      toast.success(url ? "Link copiado para a área de transferência" : "Texto copiado");
    } catch {
      toast.error("Não foi possível compartilhar nesse navegador");
    }
  };

  const handleReact = (emoji: string) => {
    closeAll();
    // Local-only reaction: copy emoji to clipboard so the agent can paste/forward.
    try {
      void navigator.clipboard.writeText(emoji);
      toast.success(`Reação copiada: ${emoji}`);
    } catch {
      /* ignore */
    }
  };

  return (
    <div className={`group/msg flex flex-col ${mine ? "items-end" : "items-start"} ${reactionChips.length > 0 ? "mb-3" : ""}`}>
      <div
        className={`relative max-w-[78%] rounded-2xl px-3 py-1.5 text-sm shadow-sm ${
          mine ? "rounded-br-sm bg-primary text-primary-foreground" : "rounded-bl-sm bg-background"
        } ${deleted ? "opacity-80" : ""}`}
      >
        {hasMenu && (
          <button
            type="button"
            aria-label="Ações da mensagem"
            onClick={() => setMenuOpen((v) => !v)}
            className={`absolute right-1 top-1 hidden rounded-full p-0.5 transition group-hover/msg:flex ${
              mine ? "text-primary-foreground/80 hover:bg-primary-foreground/15" : "text-muted-foreground hover:bg-muted"
            }`}
          >
            <ChevronDown className="h-3.5 w-3.5" />
          </button>
        )}
        {menuOpen && (
          <>
            <button
              type="button"
              aria-hidden="true"
              tabIndex={-1}
              onClick={closeAll}
              className="fixed inset-0 z-30 cursor-default bg-transparent"
            />
            <div
              className={`absolute bottom-full z-40 mb-1 min-w-[200px] overflow-hidden rounded-md border bg-popover text-popover-foreground shadow-md ${
                mine ? "right-0" : "left-0"
              }`}
            >

              {showReact && (
                <div className="border-b px-2 py-1.5">
                  {reactOpen ? (
                    <div className="flex flex-wrap gap-1">
                      {REACTION_EMOJIS.map((e) => (
                        <button
                          key={e}
                          type="button"
                          onClick={() => handleReact(e)}
                          className="rounded p-1 text-lg leading-none hover:bg-muted"
                          aria-label={`Reagir com ${e}`}
                        >
                          {e}
                        </button>
                      ))}
                    </div>
                  ) : (
                    <MenuItem
                      icon={<Smile className="h-3.5 w-3.5" />}
                      label="Reagir"
                      onClick={() => setReactOpen(true)}
                      compact
                    />
                  )}
                </div>
              )}
              {showReply && (
                <MenuItem icon={<CornerUpLeft className="h-3.5 w-3.5" />} label="Responder" onClick={() => { closeAll(); onReply!(message); }} />
              )}
              {showCopy && (
                <MenuItem icon={<Copy className="h-3.5 w-3.5" />} label="Copiar" onClick={handleCopy} />
              )}
              {showForward && (
                <MenuItem icon={<Forward className="h-3.5 w-3.5" />} label="Encaminhar" onClick={() => { closeAll(); onForward!(message); }} />
              )}
              {showDownload && (
                <MenuItem
                  icon={<Download className="h-3.5 w-3.5" />}
                  label={isDoc ? "Salvar" : "Salvar como"}
                  onClick={handleDownload}
                />
              )}
              {showEdit && (
                <MenuItem icon={<Pencil className="h-3.5 w-3.5" />} label="Editar" onClick={() => { closeAll(); onEdit!(message); }} />
              )}
              {showDelete && (
                <MenuItem
                  icon={<Trash2 className="h-3.5 w-3.5 text-destructive" />}
                  label="Apagar"
                  danger
                  onClick={() => { closeAll(); onDelete!(message); }}
                />
              )}
            </div>
          </>
        )}
        {senderLabel && (
          <div className="mb-0.5 text-[11px] font-semibold leading-tight" style={{ color: senderColor }}>
            {senderLabel}
          </div>
        )}
        {isMedia && (
          <MediaPreview message={message} mine={mine} />
        )}
        {!deleted && message.kind === "contact" && (
          <ContactCards body={message.body} mine={mine} sessionId={message.sessionId} />
        )}
        {deleted ? (
          <div className="pr-5">
            <div
              className={`mb-1 flex items-center gap-1.5 text-[12px] italic ${
                mine ? "text-primary-foreground/80" : "text-muted-foreground"
              }`}
            >
              <Ban className="h-3.5 w-3.5" />
              <span>Esta mensagem foi apagada</span>
            </div>
            {(body || restoredMediaUrl) && (() => {
              const interactive = parseInteractive(body);
              return (
                <div
                  className={`whitespace-pre-wrap break-words text-[13px] italic ${
                    mine ? "text-primary-foreground/85" : "text-muted-foreground"
                  }`}
                >
                  {message.senderName && (
                    <div className="mb-0.5 font-semibold not-italic">{message.senderName}:</div>
                  )}
                  {interactive ? <InteractiveCard data={interactive} mine={mine} /> : body}
                </div>
              );
            })()}
          </div>
        ) : (
          message.kind !== "contact" && (body || !isMedia) && (
            (() => {
              const interactive = parseInteractive(body);
              if (interactive) return <InteractiveCard data={interactive} mine={mine} />;
              return (
                <div className="whitespace-pre-wrap break-words pr-5">
                  {body || (!isMedia ? <span className="italic opacity-70">(vazio)</span> : null)}
                </div>
              );
            })()
          )
        )}

        <div className={`mt-0.5 flex items-center justify-end gap-1 text-[10px] ${mine ? "text-primary-foreground/70" : "text-muted-foreground"}`}>
          {message.edited && !deleted && <span className="italic">editada</span>}
          <span>{formatTime(message.ts)}</span>
        </div>
        {reactionChips.length > 0 && (
          <div
            className={`absolute -bottom-2.5 z-10 inline-flex items-center gap-0.5 rounded-full border bg-background px-1.5 py-0.5 text-xs leading-none shadow-sm text-foreground ${
              mine ? "right-2" : "left-2"
            }`}
            title={`${reactionChips.length} reação${reactionChips.length > 1 ? "ões" : ""}`}
          >
            {reactionChips.map((r, i) => (
              <span key={`${r.emoji}-${i}`}>{r.emoji}</span>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};

const MediaPreview = ({ message, mine }: { message: ChatMessage; mine: boolean }) => {
  const url = message.mediaUrl;
  const name = message.fileName || message.kind;
  const sizeLabel = message.fileSize ? formatBytes(message.fileSize) : "";
  const mutedTone = mine ? "text-primary-foreground/80" : "text-muted-foreground";
  const [lightboxOpen, setLightboxOpen] = useState(false);
  if (!url) {
    return (
      <div className={`mb-1 flex items-center gap-2 rounded-md border border-dashed px-2 py-1.5 text-xs ${mutedTone}`}>
        <FileText className="h-3.5 w-3.5" />
        <span className="italic">Baixando {message.kind}…</span>
      </div>
    );
  }
  if (message.kind === "image" || message.kind === "sticker") {
    return (
      <>
        <button
          type="button"
          onClick={() => setLightboxOpen(true)}
          className="mb-1 block overflow-hidden rounded-md focus:outline-none focus-visible:ring-2 focus-visible:ring-primary"
          aria-label="Abrir imagem"
        >
          <img
            src={url}
            alt={name}
            loading="lazy"
            className="max-h-72 w-auto max-w-full rounded-md object-contain transition hover:opacity-95"
          />
        </button>
        {lightboxOpen && (
          <ImageLightbox src={url} alt={name} onClose={() => setLightboxOpen(false)} />
        )}
      </>
    );
  }
  if (message.kind === "video") {
    return (
      <video
        src={url}
        controls
        preload="metadata"
        className="mb-1 max-h-72 w-full max-w-[320px] rounded-md bg-black"
      />
    );
  }
  if (message.kind === "audio") {
    return <AudioPlayer src={url} mine={message.fromMe} />;
  }
  // document / other
  return (
    <a
      href={url}
      target="_blank"
      rel="noreferrer"
      download={message.fileName || true}
      className={`mb-1 flex items-center gap-2 rounded-md border px-2 py-1.5 text-xs hover:bg-muted/40 ${
        mine ? "border-primary-foreground/30" : "border-border"
      }`}
    >
      <FileText className="h-4 w-4 shrink-0" />
      <span className="min-w-0 flex-1 truncate">{name}</span>
      {sizeLabel && <span className={`shrink-0 text-[10px] ${mutedTone}`}>{sizeLabel}</span>}
      <Download className="h-3.5 w-3.5 shrink-0" />
    </a>
  );
};

// Parses a shared contact payload coming from WhatsApp. Backend currently
// stores either a vCard string ("BEGIN:VCARD…FN:Foo…TEL…+5511…END:VCARD") or
// a numeric placeholder ("1") when only the count is known. We extract the
// name and phone so the operator can see who the contact is and call them.
function parseVCard(body: string): { name: string; phone: string } {
  const out = { name: "", phone: "" };
  if (!body) return out;
  const fn = body.match(/FN[^:]*:([^\r\n]+)/i);
  if (fn) out.name = cleanContactName(fn[1].trim());
  const waid = body.match(/waid=(\d{8,15})/i);
  if (waid) out.phone = waid[1];
  const tel = body.match(/TEL[^:]*:([^\r\n]+)/i);
  if (!out.phone && tel) out.phone = extractContactPhone(tel[1]);
  if (!out.phone) out.phone = extractContactPhone(body);
  if (!out.phone) {
    const digits = body.match(/\+?\d[\d\s().-]{6,}/);
    if (digits) out.phone = digits[0].replace(/[^\d+]/g, "");
  }
  if (!out.name && !out.phone && body.trim()) out.name = cleanContactName(body.trim());
  return out;
}

function extractContactPhone(value: string): string {
  if (!value) return "";
  const jid = value.match(/(\d{8,15})@(s\.whatsapp\.net|c\.us)/i);
  if (jid) return jid[1];
  const waid = value.match(/waid=(\d{8,15})/i);
  if (waid) return waid[1];
  const tel = value.match(/\+?\d[\d\s().-]{6,}/);
  return tel ? tel[0].replace(/[^\d+]/g, "") : "";
}

function cleanContactName(value: string): string {
  const v = (value || "").trim();
  if (!v) return "";
  if (/^\d+@(s\.whatsapp\.net|c\.us|lid)$/i.test(v)) return "";
  return v.replace(/\s*<\d+@(s\.whatsapp\.net|c\.us)>\s*$/i, "").trim();
}

const ContactCard = ({ body, mine, sessionId }: { body: string; mine: boolean; sessionId: string }) => {
  const parsed = parseVCard(body || "");
  const [name, setName] = useState(parsed.name);
  const [phone, setPhone] = useState(parsed.phone);
  const [avatarUrl, setAvatarUrl] = useState<string | undefined>(undefined);
  const [opening, setOpening] = useState(false);

  // The shared vCard sometimes carries only a LID (a 15+ digit internal
  // WhatsApp identifier like "172211528810712@lid") or a raw LID number
  // in TEL. We try to resolve it to the real E.164 phone number and
  // enrich the card with the saved contact name + avatar.
  useEffect(() => {
    let cancelled = false;
    const digits = (parsed.phone || "").replace(/\D/g, "");
    if (!digits) return;
    const looksLikeLid = digits.length >= 15 && !digits.startsWith("55");

    const enrich = async () => {
      // 1) Try LID → PN resolution when the number looks like a LID.
      let resolvedPhone = parsed.phone;
      if (looksLikeLid) {
        try {
          const r = await resolveLidPhone(sessionId, `${digits}@lid`);
          if (!cancelled && r?.phone) {
            resolvedPhone = r.phone;
            setPhone(r.phone);
          }
        } catch {
          /* ignore */
        }
      }
      // 2) Look up the saved contact (name + avatar) for either the
      //    resolved phone or the original digits.
      const q = (resolvedPhone || digits).replace(/\D/g, "");
      if (!q) return;
      try {
        const r = await listContacts({ q, kind: "user", limit: 5 });
        const match =
          (r.contacts ?? []).find((c) => c.sessionId === sessionId) ||
          r.contacts?.[0];
        if (!cancelled && match) {
          if (match.name && !parsed.name) setName(match.name);
          if (match.phone) setPhone(match.phone);
          if (match.avatarUrl) setAvatarUrl(match.avatarUrl);
        }
      } catch {
        /* ignore */
      }
    };
    void enrich();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [body, sessionId]);

  const normalizedPhone = extractContactPhone(phone) || phone.replace(/@(?:s\.whatsapp\.net|c\.us)$/i, "");
  const displayPhone = normalizedPhone && !/@lid$/i.test(normalizedPhone) ? formatPhone(normalizedPhone) : "";
  const label = name || displayPhone || "Contato compartilhado";
  const telHref = displayPhone ? `tel:${normalizedPhone.replace(/[^\d+]/g, "")}` : "";
  const copy = async () => {
    try {
      await navigator.clipboard.writeText([name, displayPhone || phone].filter(Boolean).join(" — ") || body);
      toast.success("Contato copiado");
    } catch {
      /* ignore */
    }
  };
  const openChat = async () => {
    if (!displayPhone || opening) return;
    setOpening(true);
    try {
      const digits = normalizedPhone.replace(/[^\d]/g, "");
      let targetJid = `${digits}@s.whatsapp.net`;
      try {
        const r = await listContacts({ q: digits, kind: "user", limit: 5 });
        const match = (r.contacts ?? []).find((c) => c.sessionId === sessionId) || r.contacts?.[0];
        if (match?.chatJid) targetJid = match.chatJid;
      } catch {
        /* ignore */
      }
      setActiveChat(sessionId, targetJid);
      const url = `/chats?sid=${encodeURIComponent(sessionId)}&jid=${encodeURIComponent(targetJid)}`;
      if (window.location.pathname.startsWith("/chats")) {
        window.history.pushState({}, "", url);
        window.dispatchEvent(new PopStateEvent("popstate"));
      } else {
        window.location.assign(url);
      }
    } finally {
      setOpening(false);
    }
  };
  return (
    <div
      className={`mb-1 -mx-1 overflow-hidden rounded-md border ${
        mine ? "border-primary-foreground/20 bg-primary-foreground/10" : "border-border bg-background/60"
      }`}
    >
      <div className="flex items-center gap-2 px-2.5 py-2">
        <div className={`grid h-9 w-9 shrink-0 place-items-center overflow-hidden rounded-full ${mine ? "bg-primary-foreground/20" : "bg-muted"}`}>
          {avatarUrl ? (
            // eslint-disable-next-line @next/next/no-img-element
            <img src={avatarUrl} alt={label} className="h-full w-full object-cover" />
          ) : (
            <UserIcon className="h-5 w-5" />
          )}
        </div>
        <div className="min-w-0 flex-1">
          <div className={`truncate text-sm font-semibold ${mine ? "" : "text-sky-600 dark:text-sky-400"}`}>{label}</div>
          {displayPhone && <div className="truncate text-[11px] opacity-80">{displayPhone}</div>}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {telHref && (
            <a
              href={telHref}
              title="Ligar"
              onClick={(e) => e.stopPropagation()}
              className="rounded-md p-1 hover:bg-background/40"
            >
              <Phone className="h-3.5 w-3.5" />
            </a>
          )}
          <button
            type="button"
            onClick={(e) => { e.stopPropagation(); copy(); }}
            title="Copiar contato"
            className="rounded-md p-1 hover:bg-background/40"
          >
            <Copy className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>
      <button
        type="button"
        onClick={openChat}
        disabled={!displayPhone || opening}
        title={displayPhone ? "Abrir atendimento com este contato" : undefined}
        className={`block w-full border-t py-2 text-center text-sm font-semibold transition ${
          mine ? "border-primary-foreground/20 text-primary-foreground hover:bg-primary-foreground/10" : "border-border text-sky-600 hover:bg-muted/40 dark:text-sky-400"
        } disabled:cursor-not-allowed disabled:opacity-50`}
      >
        Conversar
      </button>
    </div>
  );
};

// Renders one or many contact cards from a body that may contain multiple
// vCards separated by a "---VCARD---" marker (multi-contact share).
const ContactCards = ({ body, mine, sessionId }: { body: string; mine: boolean; sessionId: string }) => {
  const parts = (body || "")
    .split(/\n?---VCARD---\n?/)
    .map((s) => s.trim())
    .filter(Boolean);
  const list = parts.length > 0 ? parts : [body || ""];
  return (
    <div className="flex flex-col gap-1">
      {list.map((p, i) => (
        <ContactCard key={i} body={p} mine={mine} sessionId={sessionId} />
      ))}
    </div>
  );
};


function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

function suggestFileName(m: ChatMessage): string {
  const ext =
    m.kind === "image" ? "jpg" : m.kind === "video" ? "mp4" : m.kind === "audio" ? "ogg" : m.kind === "sticker" ? "webp" : "bin";
  return `${m.kind || "arquivo"}-${m.id || Date.now()}.${ext}`;
}

// Reaction chips can include the same emoji from many participants; we
// collapse duplicates for display while keeping the count badge accurate.
function dedupeReactions(
  list: Array<{ emoji: string; fromMe: boolean; senderName?: string }>,
): Array<{ emoji: string }> {
  const seen = new Set<string>();
  const out: Array<{ emoji: string }> = [];
  for (const r of list) {
    if (seen.has(r.emoji)) continue;
    seen.add(r.emoji);
    out.push({ emoji: r.emoji });
  }
  return out;
}

// senderDisplay picks the friendliest label available: stored push name,
// otherwise the digits of the participant JID (e.g. "5511…@s.whatsapp.net").
function senderDisplay(m: ChatMessage): string {
  if (m.senderName && m.senderName.trim()) return m.senderName.trim();
  const jid = m.senderJid || "";
  const at = jid.indexOf("@");
  const local = at > 0 ? jid.slice(0, at) : jid;
  const digits = local.split(":")[0];
  return digits ? `+${digits}` : "Participante";
}

// Stable per-sender hue so each participant gets a consistent color tag,
// WhatsApp-style. Uses HSL with fixed saturation/lightness for readability
// on both light and dark bubble backgrounds.
function senderHue(jid: string): string {
  let h = 0;
  for (let i = 0; i < jid.length; i++) h = (h * 31 + jid.charCodeAt(i)) >>> 0;
  return `hsl(${h % 360} 70% 55%)`;
}

const MenuItem = ({
  icon,
  label,
  onClick,
  danger,
  compact,
}: {
  icon: ReactNode;
  label: string;
  onClick: () => void;
  danger?: boolean;
  compact?: boolean;
}) => (
  <button
    type="button"
    onClick={onClick}
    className={`flex w-full items-center gap-2 ${compact ? "px-1.5 py-1" : "px-3 py-1.5"} text-left text-xs hover:bg-muted ${
      danger ? "text-destructive" : ""
    }`}
  >
    {icon}
    <span>{label}</span>
  </button>
);

// ---------------- Interactive (buttons / list) renderer ----------------
type InteractiveData = {
  variant: "buttons" | "list";
  body: string;
  footer?: string;
  buttons?: Array<{ ID?: string; Title?: string; id?: string; title?: string }>;
  buttonText?: string;
  sections?: Array<{
    Title?: string; title?: string;
    Rows?: Array<{ ID?: string; Title?: string; Description?: string; id?: string; title?: string; description?: string }>;
    rows?: Array<{ ID?: string; Title?: string; Description?: string; id?: string; title?: string; description?: string }>;
  }>;
};

function parseInteractive(body: string): InteractiveData | null {
  if (!body) return null;
  const t = body.trim();
  if (!t.startsWith("{") || t.indexOf("wa_interactive") < 0) return null;
  try {
    const o = JSON.parse(t);
    if (o && o.__type === "wa_interactive") return o as InteractiveData;
  } catch {
    return null;
  }
  return null;
}

const InteractiveCard = ({ data, mine }: { data: InteractiveData; mine: boolean }) => {
  const divider = mine ? "border-primary-foreground/25" : "border-border";
  const subtle = mine ? "text-primary-foreground/75" : "text-muted-foreground";
  return (
    <div className="pr-5">
      {data.body && <div className="whitespace-pre-wrap break-words">{data.body}</div>}
      {data.footer && <div className={`mt-1 text-[11px] ${subtle}`}>{data.footer}</div>}
      {data.variant === "buttons" && Array.isArray(data.buttons) && (
        <div className={`mt-2 flex flex-col gap-1 border-t pt-1 ${divider}`}>
          {data.buttons.map((b, i) => (
            <div
              key={i}
              className={`rounded-md px-2 py-1.5 text-center text-[13px] font-medium ${
                mine ? "bg-primary-foreground/10" : "bg-muted"
              }`}
            >
              {b.Title || b.title || b.ID || b.id || `Opção ${i + 1}`}
            </div>
          ))}
        </div>
      )}
      {data.variant === "list" && (
        <div className={`mt-2 border-t pt-1 ${divider}`}>
          <div className={`mb-1 text-[11px] uppercase tracking-wide ${subtle}`}>
            {data.buttonText || "Ver opções"}
          </div>
          <div className="flex flex-col gap-1">
            {(data.sections || []).flatMap((s, si) => {
              const rows = s.Rows || s.rows || [];
              const stitle = s.Title || s.title;
              return [
                stitle ? (
                  <div key={`s-${si}`} className={`mt-1 text-[11px] font-semibold ${subtle}`}>
                    {stitle}
                  </div>
                ) : null,
                ...rows.map((r, ri) => (
                  <div
                    key={`r-${si}-${ri}`}
                    className={`rounded-md px-2 py-1 text-[13px] ${
                      mine ? "bg-primary-foreground/10" : "bg-muted"
                    }`}
                  >
                    <div className="font-medium">{r.Title || r.title || r.ID || r.id}</div>
                    {(r.Description || r.description) && (
                      <div className={`text-[11px] ${subtle}`}>{r.Description || r.description}</div>
                    )}
                  </div>
                )),
              ].filter(Boolean) as ReactNode[];
            })}
          </div>
        </div>
      )}
    </div>
  );
};
