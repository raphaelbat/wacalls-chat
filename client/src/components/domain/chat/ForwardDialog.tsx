import { useMemo, useState } from "react";
import { Forward, Search, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { ChatMessage, ChatSummary } from "@/types/chat";
import { formatPeer, previewBody } from "./format";

interface Props {
  sessionId: string;
  currentJid: string | null;
  message: ChatMessage;
  chats: ChatSummary[];
  onClose: () => void;
  onSubmit: (targets: string[]) => void | Promise<void>;
}

/**
 * Forward dialog. Lists every conversation in the current session, lets the
 * agent pick one or more recipients, and submits their JIDs to the parent.
 */
export const ForwardDialog = ({ currentJid, message, chats, onClose, onSubmit }: Props) => {
  const [filter, setFilter] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [submitting, setSubmitting] = useState(false);

  const items = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return chats
      .filter((c) => c.chatJid !== currentJid)
      .filter((c) => {
        if (!q) return true;
        const name = (c.name ?? formatPeer(c.chatJid)).toLowerCase();
        return name.includes(q) || c.chatJid.toLowerCase().includes(q);
      });
  }, [chats, filter, currentJid]);

  const toggle = (jid: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(jid)) next.delete(jid);
      else next.add(jid);
      return next;
    });
  };

  const handleSend = async () => {
    if (selected.size === 0 || submitting) return;
    setSubmitting(true);
    try {
      await onSubmit(Array.from(selected));
    } finally {
      setSubmitting(false);
    }
  };

  const preview = message.kind === "text" ? message.body : previewBody(message.kind, message.body);

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/50 p-4" onClick={onClose}>
      <div className="flex w-full max-w-md flex-col rounded-lg border bg-card shadow-xl" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between border-b px-4 py-2.5">
          <div className="flex items-center gap-2 text-sm font-semibold">
            <Forward className="h-4 w-4" /> Encaminhar mensagem
          </div>
          <button onClick={onClose} className="rounded-md p-1 text-muted-foreground hover:bg-muted">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="border-b bg-muted/40 px-4 py-2 text-xs">
          <div className="text-[10px] uppercase tracking-wide text-muted-foreground">Mensagem</div>
          <div className="mt-0.5 line-clamp-2 whitespace-pre-wrap break-words">{preview || <span className="italic opacity-70">(vazio)</span>}</div>
        </div>

        <div className="border-b px-3 py-2">
          <div className="flex items-center gap-2 rounded-md border bg-background px-2 py-1">
            <Search className="h-3.5 w-3.5 text-muted-foreground" />
            <input
              autoFocus
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Buscar conversa…"
              className="w-full bg-transparent text-xs outline-none"
            />
          </div>
        </div>

        <ul className="max-h-72 overflow-y-auto">
          {items.length === 0 ? (
            <li className="px-4 py-6 text-center text-xs text-muted-foreground">Nenhuma conversa encontrada.</li>
          ) : (
            items.map((c) => {
              const name = c.name && c.name.trim() ? c.name : formatPeer(c.chatJid);
              const checked = selected.has(c.chatJid);
              return (
                <li key={c.chatJid}>
                  <button
                    type="button"
                    onClick={() => toggle(c.chatJid)}
                    className={`flex w-full items-center gap-3 border-b px-3 py-2 text-left text-sm hover:bg-muted/60 ${checked ? "bg-muted" : ""}`}
                  >
                    <span className="relative grid h-8 w-8 shrink-0 place-items-center overflow-hidden rounded-full bg-primary/10 text-xs font-semibold text-primary">
                      {c.avatarUrl ? (
                        <img src={c.avatarUrl} alt={name} className="h-full w-full object-cover" loading="lazy" />
                      ) : (
                        name.slice(0, 1).toUpperCase()
                      )}
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm font-medium">{name}</div>
                      <div className="truncate text-[10px] text-muted-foreground">{formatPeer(c.chatJid)}</div>
                    </div>
                    <span
                      className={`grid h-4 w-4 place-items-center rounded border text-[10px] ${
                        checked ? "border-primary bg-primary text-primary-foreground" : "border-muted-foreground/40"
                      }`}
                      aria-hidden
                    >
                      {checked ? "✓" : ""}
                    </span>
                  </button>
                </li>
              );
            })
          )}
        </ul>

        <div className="flex items-center justify-between border-t px-4 py-2.5">
          <div className="text-xs text-muted-foreground">{selected.size} destinatário(s)</div>
          <div className="flex gap-2">
            <Button size="sm" variant="ghost" onClick={onClose} disabled={submitting}>Cancelar</Button>
            <Button size="sm" onClick={() => void handleSend()} disabled={selected.size === 0 || submitting}>
              {submitting ? "Enviando…" : "Encaminhar"}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
};
