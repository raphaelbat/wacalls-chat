import { useEffect, useRef, useState } from "react";
import { FileText, Image as ImageIcon, Mic, Paperclip, Plus, Trash2, Video, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  createQuickReply,
  deleteQuickReply,
  listQuickReplies,
  removeQuickReplyMedia,
  updateQuickReply,
  uploadQuickReplyMedia,
  type QuickReply,
  type QuickReplyKind,
} from "@/services/quickReplies";
import { QUICK_REPLY_VARIABLES } from "@/lib/quick-reply-vars";

type Props = {
  open: boolean;
  onClose: () => void;
  onChanged?: () => void;
};

const emptyDraft = { shortcut: "", title: "", content: "", global: true };

const MAX_MEDIA_BYTES = 32 * 1024 * 1024;

const kindIcon = (kind?: QuickReplyKind) => {
  switch (kind) {
    case "image":
      return ImageIcon;
    case "video":
      return Video;
    case "audio":
      return Mic;
    default:
      return FileText;
  }
};

const humanSize = (bytes?: number): string => {
  if (!bytes) return "";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
};

export const QuickReplyManager = ({ open, onClose, onChanged }: Props) => {
  const [items, setItems] = useState<QuickReply[]>([]);
  const [draft, setDraft] = useState(emptyDraft);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // Arquivo recém-escolhido e o anexo que já está salvo no servidor (na edição).
  const [file, setFile] = useState<File | null>(null);
  const [savedMedia, setSavedMedia] = useState<QuickReply | null>(null);
  const [error, setError] = useState("");
  const fileInputRef = useRef<HTMLInputElement>(null);

  const reload = async () => {
    try {
      setItems(await listQuickReplies());
    } catch {
      setItems([]);
    }
  };

  useEffect(() => {
    if (open) void reload();
  }, [open]);

  if (!open) return null;

  const resetForm = () => {
    setDraft(emptyDraft);
    setEditingId(null);
    setFile(null);
    setSavedMedia(null);
    setError("");
    if (fileInputRef.current) fileInputRef.current.value = "";
  };

  const pickFile = (picked: File | null) => {
    if (!picked) return;
    if (picked.size > MAX_MEDIA_BYTES) {
      setError("O anexo precisa ter no máximo 32 MB.");
      return;
    }
    setError("");
    setFile(picked);
  };

  const submit = async () => {
    // Vale com texto, com anexo, ou com os dois — só o atalho é obrigatório.
    const hasMedia = !!file || !!savedMedia?.mediaUrl;
    if (!draft.shortcut.trim() || (!draft.content.trim() && !hasMedia)) {
      setError("Informe o atalho e um texto ou um anexo.");
      return;
    }
    setBusy(true);
    setError("");
    try {
      let id = editingId;
      if (id) await updateQuickReply(id, draft);
      else id = (await createQuickReply(draft)).id;
      if (file && id) await uploadQuickReplyMedia(id, file);
      resetForm();
      await reload();
      onChanged?.();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Não consegui salvar a resposta rápida.");
    } finally {
      setBusy(false);
    }
  };

  const dropSavedMedia = async () => {
    if (!editingId) return;
    setBusy(true);
    try {
      await removeQuickReplyMedia(editingId);
      setSavedMedia(null);
      await reload();
      onChanged?.();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Não consegui remover o anexo.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/50 p-4" onClick={onClose}>
      <div
        className="flex max-h-[85vh] w-full max-w-2xl flex-col overflow-hidden rounded-lg border bg-card shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b px-4 py-3">
          <div className="text-sm font-semibold">Respostas rápidas</div>
          <button onClick={onClose} className="rounded-md p-1 text-muted-foreground hover:bg-muted">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="grid gap-3 border-b p-4">
          <div className="grid grid-cols-2 gap-2">
            <input
              value={draft.shortcut}
              onChange={(e) => setDraft((d) => ({ ...d, shortcut: e.target.value }))}
              placeholder="atalho (ex: bomdia)"
              className="rounded-md border bg-background px-2 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
            />
            <input
              value={draft.title}
              onChange={(e) => setDraft((d) => ({ ...d, title: e.target.value }))}
              placeholder="título (opcional)"
              className="rounded-md border bg-background px-2 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
            />
          </div>
          <textarea
            value={draft.content}
            onChange={(e) => setDraft((d) => ({ ...d, content: e.target.value }))}
            rows={3}
            placeholder="Olá {{primeiro_nome}}, aqui é {{atendente}}. Protocolo {{protocolo}}."
            className="resize-none rounded-md border bg-background px-2 py-1.5 text-sm outline-none focus:ring-2 focus:ring-ring"
          />
          <div className="flex flex-wrap items-center gap-1 text-[10px] text-muted-foreground">
            <span>Variáveis:</span>
            {QUICK_REPLY_VARIABLES.map((v) => (
              <button
                key={v}
                type="button"
                onClick={() => setDraft((d) => ({ ...d, content: `${d.content}{{${v}}}` }))}
                className="rounded border px-1 py-0.5 font-mono hover:bg-muted"
              >
                {`{{${v}}}`}
              </button>
            ))}
          </div>
          {/* Anexo: imagem, vídeo, áudio ou documento enviado junto com o texto. */}
          <div className="flex flex-wrap items-center gap-2">
            <input
              ref={fileInputRef}
              type="file"
              className="hidden"
              onChange={(e) => pickFile(e.target.files?.[0] ?? null)}
            />
            <Button size="sm" variant="outline" type="button" onClick={() => fileInputRef.current?.click()}>
              <Paperclip className="mr-1 h-3.5 w-3.5" />
              {file || savedMedia?.mediaUrl ? "Trocar anexo" : "Anexar mídia"}
            </Button>

            {file ? (
              <span className="flex items-center gap-1 rounded-md border bg-muted/40 px-2 py-1 text-xs">
                <span className="max-w-[220px] truncate">{file.name}</span>
                <span className="text-muted-foreground">{humanSize(file.size)}</span>
                <button
                  type="button"
                  onClick={() => {
                    setFile(null);
                    if (fileInputRef.current) fileInputRef.current.value = "";
                  }}
                  className="rounded p-0.5 text-muted-foreground hover:text-destructive"
                  aria-label="Remover arquivo selecionado"
                >
                  <X className="h-3 w-3" />
                </button>
              </span>
            ) : savedMedia?.mediaUrl ? (
              <span className="flex items-center gap-1 rounded-md border bg-muted/40 px-2 py-1 text-xs">
                <span className="max-w-[220px] truncate">{savedMedia.mediaName || "anexo"}</span>
                <span className="text-muted-foreground">{humanSize(savedMedia.mediaSize)}</span>
                <button
                  type="button"
                  onClick={() => void dropSavedMedia()}
                  disabled={busy}
                  className="rounded p-0.5 text-muted-foreground hover:text-destructive"
                  aria-label="Remover anexo salvo"
                >
                  <Trash2 className="h-3 w-3" />
                </button>
              </span>
            ) : (
              <span className="text-[10px] text-muted-foreground">
                Opcional — até 32 MB. O texto vai como legenda da mídia.
              </span>
            )}
          </div>

          {error && <div className="text-xs text-destructive">{error}</div>}

          <div className="flex items-center justify-between">
            <label className="flex items-center gap-2 text-xs text-muted-foreground">
              <input
                type="checkbox"
                checked={draft.global}
                onChange={(e) => setDraft((d) => ({ ...d, global: e.target.checked }))}
              />
              Compartilhar com a equipe
            </label>
            <div className="flex gap-2">
              {editingId && (
                <Button size="sm" variant="ghost" onClick={resetForm}>
                  Cancelar
                </Button>
              )}
              <Button size="sm" onClick={() => void submit()} disabled={busy}>
                <Plus className="mr-1 h-3.5 w-3.5" />
                {editingId ? "Salvar" : "Adicionar"}
              </Button>
            </div>
          </div>
        </div>

        <div className="flex-1 overflow-y-auto p-2">
          {items.length === 0 && (
            <div className="p-4 text-center text-xs text-muted-foreground">Nenhuma resposta rápida cadastrada.</div>
          )}
          {items.map((it) => {
            const KindIcon = kindIcon(it.mediaKind);
            return (
            <div key={it.id} className="flex items-start gap-2 rounded-md px-2 py-2 hover:bg-muted/60">
              <button
                type="button"
                className="min-w-0 flex-1 text-left"
                onClick={() => {
                  setEditingId(it.id);
                  setDraft({ shortcut: it.shortcut, title: it.title, content: it.content, global: it.global });
                  setSavedMedia(it);
                  setFile(null);
                  setError("");
                  if (fileInputRef.current) fileInputRef.current.value = "";
                }}
              >
                <div className="flex items-center gap-2 text-sm">
                  <span className="rounded bg-primary/10 px-1.5 py-0.5 font-mono text-xs text-primary">/{it.shortcut}</span>
                  <span className="truncate font-medium">{it.title}</span>
                  {it.mediaUrl && (
                    <span className="flex shrink-0 items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                      <KindIcon className="h-3 w-3" />
                      {it.mediaName || "anexo"}
                    </span>
                  )}
                  {!it.global && <span className="text-[10px] text-muted-foreground">pessoal</span>}
                </div>
                <div className="mt-0.5 line-clamp-2 whitespace-pre-wrap text-xs text-muted-foreground">{it.content}</div>
              </button>
              <button
                type="button"
                onClick={async () => {
                  await deleteQuickReply(it.id);
                  await reload();
                  onChanged?.();
                }}
                className="rounded-md p-1.5 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                aria-label="Excluir"
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
            );
          })}
        </div>
      </div>
    </div>
  );
};