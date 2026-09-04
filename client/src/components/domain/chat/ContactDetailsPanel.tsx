import { useEffect, useMemo, useState } from "react";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Copy,
  Image as ImageIcon,
  Film,
  Mic,
  FileText,
  RefreshCw,
  History as HistoryIcon,
  StickyNote,
  BarChart3,
  Phone,
  Hash,
  Calendar,
  MessageCircle,
  Search,
  X,
} from "lucide-react";
import type { ChatClosure, ChatMessage, ChatSummary } from "@/types/chat";
import { listChatClosures, resolveLidPhone, syncChatContact } from "@/services/chats";
import { formatPeer, formatTime, isGroupJid } from "./format";
import { formatPhone } from "@/lib/phone-format";
import { ChatTagsManager } from "./ChatTagsManager";
import type { Tag } from "@/types/tag";
import { tagChipStyle } from "@/lib/tag-color";
import { EditContactDialog } from "./EditContactDialog";
import { Pencil } from "lucide-react";

interface Props {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  sessionId: string;
  chatJid: string;
  chat?: ChatSummary;
  messages: ChatMessage[];
  onTagsChange?: (tags: Tag[]) => void;
}

type Tab = "summary" | "media" | "notes" | "history";
type KindFilter = "all" | "text" | "image" | "video" | "audio" | "document";

const KIND_CHIPS: { id: KindFilter; label: string; icon?: typeof ImageIcon }[] = [
  { id: "all", label: "Tudo" },
  { id: "text", label: "Texto", icon: MessageCircle },
  { id: "image", label: "Imagens", icon: ImageIcon },
  { id: "video", label: "Vídeos", icon: Film },
  { id: "audio", label: "Áudios", icon: Mic },
  { id: "document", label: "Arquivos", icon: FileText },
];

// highlight wraps query matches in <mark>; safely handles empty / regex chars.
const highlight = (text: string, q: string) => {
  if (!q.trim()) return text;
  const safe = q.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const parts = text.split(new RegExp(`(${safe})`, "ig"));
  return parts.map((p, i) =>
    p.toLowerCase() === q.toLowerCase() ? (
      <mark key={i} className="rounded bg-amber-200/70 px-0.5 text-foreground dark:bg-amber-500/40">
        {p}
      </mark>
    ) : (
      <span key={i}>{p}</span>
    ),
  );
};

const STATUS_BADGE: Record<string, { label: string; cls: string }> = {
  waiting: { label: "Aguardando", cls: "bg-amber-500/15 text-amber-600 border-amber-500/30" },
  open: { label: "Em atendimento", cls: "bg-emerald-500/15 text-emerald-600 border-emerald-500/30" },
  closed: { label: "Encerrado", cls: "bg-muted text-muted-foreground border-border" },
  group: { label: "Grupo", cls: "bg-sky-500/15 text-sky-600 border-sky-500/30" },
};

const fmtDate = (ts: number) => {
  if (!ts) return "—";
  const d = new Date(ts);
  return `${d.toLocaleDateString()} ${formatTime(ts)}`;
};

const fmtBytes = (n?: number) => {
  if (!n) return "";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
};

export const ContactDetailsPanel = ({
  open,
  onOpenChange,
  sessionId,
  chatJid,
  chat,
  messages,
  onTagsChange,
}: Props) => {
  const [tab, setTab] = useState<Tab>("summary");
  const [kindFilter, setKindFilter] = useState<KindFilter>("all");
  const [query, setQuery] = useState("");
  const [syncing, setSyncing] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [notes, setNotes] = useState("");
  const [savedAt, setSavedAt] = useState<number | null>(null);
  const [closures, setClosures] = useState<ChatClosure[]>([]);
  const [loadingHistory, setLoadingHistory] = useState(false);
  const [chatTags, setChatTags] = useState<Tag[]>([]);
  const [activeTagId, setActiveTagId] = useState<string | null>(null);

  const isGroup = !!chat?.isGroup || isGroupJid(chatJid);
  const displayName = chat?.name?.trim() || formatPeer(chatJid);
  // For @lid JIDs we can't derive the phone locally — the local part is the
  // WhatsApp hidden-user ID, not the real number. Ask the backend to resolve
  // it via the LID↔PN store and cache the result while the panel is open.
  const [resolvedPhone, setResolvedPhone] = useState<string | null>(null);
  useEffect(() => {
    setResolvedPhone(null);
    if (!open || !chatJid.endsWith("@lid") || !sessionId) return;
    let cancelled = false;
    void resolveLidPhone(sessionId, chatJid).then((r) => {
      if (!cancelled && r?.phone) setResolvedPhone(r.phone);
    });
    return () => {
      cancelled = true;
    };
  }, [open, sessionId, chatJid]);
  const phone = useMemo(() => {
    if (chatJid.endsWith("@lid")) {
      return resolvedPhone ? formatPhone(resolvedPhone) : "Número oculto";
    }
    return formatPeer(chatJid);
  }, [chatJid, resolvedPhone]);
  const status = chat?.status ?? "open";

  // Reset the tag filter whenever the panel changes contact.
  useEffect(() => {
    setActiveTagId(null);
  }, [chatJid, sessionId]);
  const activeTag = chatTags.find((t) => t.id === activeTagId) || null;

  // Notes persistence (local-only, per chat)
  const noteKey = `voipinho.contact-notes.${sessionId}:${chatJid}`;
  useEffect(() => {
    if (!open) return;
    try {
      setNotes(localStorage.getItem(noteKey) ?? "");
    } catch {
      setNotes("");
    }
    setSavedAt(null);
  }, [noteKey, open]);
  useEffect(() => {
    if (!open) return;
    const t = window.setTimeout(() => {
      try {
        localStorage.setItem(noteKey, notes);
        setSavedAt(Date.now());
      } catch {
        // ignore
      }
    }, 400);
    return () => window.clearTimeout(t);
  }, [notes, noteKey, open]);

  // Stats
  const stats = useMemo(() => {
    let inbound = 0;
    let outbound = 0;
    const kinds: Record<string, number> = {};
    let first = 0;
    let last = 0;
    for (const m of messages) {
      if (m.fromMe) outbound++;
      else inbound++;
      kinds[m.kind] = (kinds[m.kind] ?? 0) + 1;
      if (!first || m.ts < first) first = m.ts;
      if (m.ts > last) last = m.ts;
    }
    return { inbound, outbound, kinds, first, last, total: messages.length };
  }, [messages]);

  // Search: matches body, fileName, sender, mime
  // If a tag is selected, its name acts as an additional implicit query — so
  // clicking a tag chip narrows mídias / notas / mensagens that mention it.
  const effectiveQuery = activeTag ? `${activeTag.name} ${query}`.trim() : query.trim();
  const q = effectiveQuery.toLowerCase();
  const matchQuery = (m: ChatMessage) => {
    if (!q) return true;
    return (
      (m.body ?? "").toLowerCase().includes(q) ||
      (m.fileName ?? "").toLowerCase().includes(q) ||
      (m.senderName ?? "").toLowerCase().includes(q) ||
      (m.mediaMime ?? "").toLowerCase().includes(q)
    );
  };
  const matchKind = (m: ChatMessage, only: "media" | "any") => {
    const isMedia = ["image", "video", "audio", "document"].includes(m.kind);
    if (kindFilter === "all") return only === "media" ? isMedia : true;
    if (kindFilter === "text") return only === "media" ? false : m.kind === "text";
    return m.kind === kindFilter;
  };
  const mediaItems = useMemo(
    () => messages.filter((m) => matchKind(m, "media") && matchQuery(m)),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [messages, kindFilter, q],
  );
  const searchHits = useMemo(
    () => (q ? messages.filter((m) => matchKind(m, "any") && matchQuery(m)) : []),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [messages, kindFilter, q],
  );
  const notesMatchCount = useMemo(() => {
    if (!q || !notes) return 0;
    const lower = notes.toLowerCase();
    let i = 0;
    let n = 0;
    while ((i = lower.indexOf(q, i)) !== -1) {
      n++;
      i += q.length;
    }
    return n;
  }, [notes, q]);

  // History
  useEffect(() => {
    if (!open || tab !== "history") return;
    let cancelled = false;
    setLoadingHistory(true);
    listChatClosures(sessionId, chatJid)
      .then((rows) => {
        if (!cancelled) setClosures(rows ?? []);
      })
      .catch(() => {
        if (!cancelled) setClosures([]);
      })
      .finally(() => {
        if (!cancelled) setLoadingHistory(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open, tab, sessionId, chatJid]);

  const copy = (val: string) => {
    try {
      void navigator.clipboard.writeText(val);
    } catch {
      // ignore
    }
  };

  const handleSync = async () => {
    setSyncing(true);
    try {
      await syncChatContact(sessionId, chatJid);
    } catch {
      // ignore
    } finally {
      setSyncing(false);
    }
  };

  const statusInfo = STATUS_BADGE[status] ?? STATUS_BADGE.open;

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-full p-0 sm:max-w-md">
        <ScrollArea className="h-full">
          <SheetHeader className="space-y-0 border-b bg-gradient-to-b from-primary/10 via-primary/5 to-transparent px-6 pb-6 pt-8 text-center">
            <div className="mx-auto grid h-24 w-24 place-items-center overflow-hidden rounded-full bg-primary/10 text-3xl font-semibold text-primary ring-4 ring-background shadow-sm">
              {chat?.avatarUrl ? (
                <img
                  src={chat.avatarUrl}
                  alt={displayName}
                  className="h-full w-full object-cover"
                  onError={(e) => {
                    (e.currentTarget as HTMLImageElement).style.display = "none";
                  }}
                />
              ) : (
                displayName.slice(0, 1).toUpperCase()
              )}
            </div>
            <SheetTitle className="mt-3 truncate text-center text-lg font-semibold">{displayName}</SheetTitle>
            <SheetDescription className="flex items-center justify-center gap-2">
              <Badge variant="outline" className={`border ${statusInfo.cls}`}>{statusInfo.label}</Badge>
              {isGroup && <Badge variant="outline">Grupo</Badge>}
            </SheetDescription>
            <div className="mt-4 flex items-center justify-center gap-2">
              <Button size="sm" variant="outline" onClick={handleSync} disabled={syncing}>
                <RefreshCw className={`mr-1.5 h-3.5 w-3.5 ${syncing ? "animate-spin" : ""}`} />
                Atualizar contato
              </Button>
              {!isGroup && (
                <Button size="sm" variant="outline" onClick={() => setEditOpen(true)}>
                  <Pencil className="mr-1.5 h-3.5 w-3.5" />
                  Editar contato
                </Button>
              )}
            </div>
          </SheetHeader>

          <EditContactDialog
            open={editOpen}
            onOpenChange={setEditOpen}
            sessionId={sessionId}
            chatJid={chatJid}
            currentName={displayName}
            currentAvatarUrl={chat?.avatarUrl}
            onSaved={() => {
              setEditOpen(false);
              void syncChatContact(sessionId, chatJid).catch(() => {});
            }}
          />

          <div className="space-y-1 border-b px-6 py-4 text-sm">
            <Row icon={<Phone className="h-3.5 w-3.5" />} label="Telefone" value={phone} onCopy={() => copy(phone)} />
            <Row icon={<Hash className="h-3.5 w-3.5" />} label="JID" value={chatJid} onCopy={() => copy(chatJid)} mono />
            <Row icon={<MessageCircle className="h-3.5 w-3.5" />} label="Mensagens" value={String(stats.total)} />
            <Row
              icon={<Calendar className="h-3.5 w-3.5" />}
              label="Primeira interação"
              value={fmtDate(stats.first)}
            />
            <Row
              icon={<Calendar className="h-3.5 w-3.5" />}
              label="Última interação"
              value={fmtDate(stats.last)}
            />
          </div>


          <div className="sticky top-0 z-10 space-y-2 border-b bg-background/95 px-3 pb-2 pt-3 backdrop-blur">
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Buscar mídias, notas e mensagens…"
                className="w-full rounded-md border bg-background py-1.5 pl-8 pr-8 text-xs focus:outline-none focus:ring-2 focus:ring-primary/40"
              />
              {query && (
                <button
                  type="button"
                  onClick={() => setQuery("")}
                  className="absolute right-1.5 top-1/2 grid h-5 w-5 -translate-y-1/2 place-items-center rounded text-muted-foreground hover:bg-muted"
                  title="Limpar busca"
                >
                  <X className="h-3 w-3" />
                </button>
              )}
            </div>
            <div className="flex flex-wrap gap-1">
              {KIND_CHIPS.map((c) => {
                const Icon = c.icon;
                return (
                  <button
                    key={c.id}
                    type="button"
                    onClick={() => setKindFilter(c.id)}
                    className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[10px] transition ${
                      kindFilter === c.id
                        ? "border-primary bg-primary text-primary-foreground"
                        : "border-border bg-background hover:bg-muted"
                    }`}
                  >
                    {Icon && <Icon className="h-2.5 w-2.5" />}
                    {c.label}
                  </button>
                );
              })}
            </div>
            <div className="flex">
              {([
                ["summary", "Resumo", BarChart3, q ? searchHits.length : null],
                ["media", "Mídias", ImageIcon, mediaItems.length],
                ["notes", "Notas", StickyNote, q ? notesMatchCount : null],
                ["history", "Histórico", HistoryIcon, null],
              ] as const).map(([id, label, Icon, count]) => (
                <button
                  key={id}
                  onClick={() => setTab(id)}
                  className={`flex flex-1 items-center justify-center gap-1 border-b-2 px-1 py-2 text-xs font-medium transition ${
                    tab === id
                      ? "border-primary text-primary"
                      : "border-transparent text-muted-foreground hover:text-foreground"
                  }`}
                >
                  <Icon className="h-3.5 w-3.5" />
                  {label}
                  {count !== null && count > 0 && (
                    <span className="ml-0.5 rounded-full bg-primary/15 px-1.5 text-[9px] font-semibold text-primary">
                      {count}
                    </span>
                  )}
                </button>
              ))}
            </div>
          </div>

          {tab === "summary" && (
            <div className="space-y-3 px-6 py-4">
              <div className="grid grid-cols-2 gap-2">
                <StatCard label="Recebidas" value={stats.inbound} tone="emerald" />
                <StatCard label="Enviadas" value={stats.outbound} tone="sky" />
                <StatCard label="Imagens" value={stats.kinds.image ?? 0} tone="violet" />
                <StatCard label="Vídeos" value={stats.kinds.video ?? 0} tone="rose" />
                <StatCard label="Áudios" value={stats.kinds.audio ?? 0} tone="amber" />
                <StatCard label="Documentos" value={stats.kinds.document ?? 0} tone="slate" />
              </div>
              {q ? (
                <div className="space-y-2">
                  <div className="text-[11px] font-medium text-muted-foreground">
                    {searchHits.length} resultado{searchHits.length === 1 ? "" : "s"} para “{query}”
                  </div>
                  {searchHits.length === 0 ? (
                    <div className="grid h-20 place-items-center rounded border border-dashed text-xs text-muted-foreground">
                      Nada encontrado.
                    </div>
                  ) : (
                    <ul className="space-y-1.5">
                      {searchHits.slice(0, 30).map((m) => (
                        <li
                          key={m.id}
                          className="rounded-md border bg-muted/30 px-2.5 py-1.5 text-[11px]"
                        >
                          <div className="flex items-center justify-between gap-2 text-[10px] text-muted-foreground">
                            <span className="truncate">
                              {m.fromMe ? "Você" : m.senderName || "Contato"} · {m.kind}
                            </span>
                            <span>{fmtDate(m.ts)}</span>
                          </div>
                          <div className="mt-0.5 line-clamp-2">
                            {highlight(m.body || m.fileName || "(mídia sem legenda)", query)}
                          </div>
                        </li>
                      ))}
                      {searchHits.length > 30 && (
                        <li className="text-center text-[10px] text-muted-foreground">
                          + {searchHits.length - 30} resultado(s)…
                        </li>
                      )}
                    </ul>
                  )}
                </div>
              ) : (
                <div className="rounded-lg border bg-muted/30 p-3 text-xs text-muted-foreground">
                  Sessão: <span className="font-mono text-foreground">{sessionId.slice(0, 8)}…</span>
                </div>
              )}
            </div>
          )}

          {tab === "media" && (
            <div className="px-4 py-3">
              {mediaItems.length === 0 ? (
                <div className="grid h-32 place-items-center text-xs text-muted-foreground">
                  {q || kindFilter !== "all"
                    ? "Nenhuma mídia corresponde ao filtro."
                    : "Nenhuma mídia neste atendimento."}
                </div>
              ) : (
                <div className="grid grid-cols-3 gap-2">
                  {mediaItems.map((m) => (
                    <MediaTile key={m.id} m={m} />
                  ))}
                </div>
              )}
            </div>
          )}

          {tab === "notes" && (
            <div className="space-y-2 px-6 py-4">
              <label className="text-xs font-medium text-muted-foreground">
                Anotações internas (visível apenas para você)
                {q && (
                  <span className="ml-2 text-[10px] text-primary">
                    {notesMatchCount} ocorrência{notesMatchCount === 1 ? "" : "s"}
                  </span>
                )}
              </label>
              {q && notes && notesMatchCount > 0 && (
                <div className="rounded-md border bg-muted/30 p-2 text-xs leading-relaxed">
                  {highlight(notes, query)}
                </div>
              )}
              <textarea
                className="min-h-[200px] w-full rounded-lg border bg-background p-3 text-sm focus:outline-none focus:ring-2 focus:ring-primary/40"
                placeholder="Ex.: cliente preferiu retorno após 14h, pediu desconto..."
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
              />
              <div className="flex items-center justify-between text-[11px] text-muted-foreground">
                <span>Salvo automaticamente.</span>
                {savedAt && <span>Última gravação às {formatTime(savedAt)}</span>}
              </div>
            </div>
          )}

          {tab === "history" && (
            <div className="space-y-2 px-6 py-4">
              {loadingHistory ? (
                <div className="text-xs text-muted-foreground">Carregando histórico…</div>
              ) : closures.length === 0 ? (
                <div className="grid h-24 place-items-center text-xs text-muted-foreground">
                  Nenhum encerramento registrado.
                </div>
              ) : (
                <ol className="space-y-3">
                  {closures.map((c) => (
                    <li key={c.id} className="rounded-lg border bg-muted/30 p-3 text-xs">
                      <div className="flex items-center justify-between">
                        <span className="font-medium">{c.userEmail || c.userId}</span>
                        <span className="text-muted-foreground">{fmtDate(c.closedAt)}</span>
                      </div>
                      {c.reason && <div className="mt-1 text-muted-foreground">{c.reason}</div>}
                    </li>
                  ))}
                </ol>
              )}
            </div>
          )}
        </ScrollArea>
      </SheetContent>
    </Sheet>
  );
};

interface RowProps {
  icon: React.ReactNode;
  label: string;
  value: string;
  onCopy?: () => void;
  mono?: boolean;
}
const Row = ({ icon, label, value, onCopy, mono }: RowProps) => (
  <div className="flex items-center gap-2 py-1.5">
    <span className="grid h-6 w-6 shrink-0 place-items-center rounded-md bg-muted text-muted-foreground">
      {icon}
    </span>
    <div className="min-w-0 flex-1">
      <div className="text-[10px] uppercase tracking-wide text-muted-foreground">{label}</div>
      <div className={`truncate text-xs ${mono ? "font-mono" : "font-medium"}`}>{value}</div>
    </div>
    {onCopy && (
      <Button size="sm" variant="ghost" className="h-7 w-7 p-0" onClick={onCopy} title="Copiar">
        <Copy className="h-3.5 w-3.5" />
      </Button>
    )}
  </div>
);

const TONES: Record<string, string> = {
  emerald: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  sky: "bg-sky-500/10 text-sky-700 dark:text-sky-300",
  violet: "bg-violet-500/10 text-violet-700 dark:text-violet-300",
  rose: "bg-rose-500/10 text-rose-700 dark:text-rose-300",
  amber: "bg-amber-500/10 text-amber-700 dark:text-amber-300",
  slate: "bg-slate-500/10 text-slate-700 dark:text-slate-300",
};

const StatCard = ({ label, value, tone }: { label: string; value: number; tone: string }) => (
  <div className={`rounded-lg border p-3 ${TONES[tone] ?? ""}`}>
    <div className="text-[10px] uppercase tracking-wide opacity-70">{label}</div>
    <div className="mt-1 text-xl font-semibold tabular-nums">{value}</div>
  </div>
);

const MediaTile = ({ m }: { m: ChatMessage }) => {
  if (m.kind === "image" && m.mediaUrl) {
    return (
      <a
        href={m.mediaUrl}
        target="_blank"
        rel="noreferrer"
        className="group relative aspect-square overflow-hidden rounded-md border bg-muted"
      >
        <img src={m.mediaUrl} alt="" className="h-full w-full object-cover transition group-hover:scale-105" loading="lazy" />
      </a>
    );
  }
  if (m.kind === "video" && m.mediaUrl) {
    return (
      <a
        href={m.mediaUrl}
        target="_blank"
        rel="noreferrer"
        className="relative grid aspect-square place-items-center overflow-hidden rounded-md border bg-black text-white"
      >
        <Film className="h-6 w-6 opacity-80" />
      </a>
    );
  }
  if (m.kind === "audio" && m.mediaUrl) {
    return (
      <a
        href={m.mediaUrl}
        target="_blank"
        rel="noreferrer"
        className="grid aspect-square place-items-center rounded-md border bg-amber-500/10 text-amber-600"
      >
        <Mic className="h-6 w-6" />
      </a>
    );
  }
  return (
    <a
      href={m.mediaUrl || "#"}
      target="_blank"
      rel="noreferrer"
      className="flex aspect-square flex-col items-center justify-center gap-1 rounded-md border bg-muted/50 p-2 text-center"
    >
      <FileText className="h-5 w-5 text-muted-foreground" />
      <span className="line-clamp-2 text-[10px]">{m.fileName || "arquivo"}</span>
      {m.fileSize ? <span className="text-[10px] text-muted-foreground">{fmtBytes(m.fileSize)}</span> : null}
    </a>
  );
};