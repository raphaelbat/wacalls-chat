import { useEffect, useMemo, useState } from "react";
import { Plus, X, Tag as TagIcon, Check } from "lucide-react";
import type { Tag } from "@/types/tag";
import { listTags, attachChatTag, detachChatTag, listChatTags } from "@/services/tags";
import { tagChipStyle } from "@/lib/tag-color";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Button } from "@/components/ui/button";

interface Props {
  sessionId: string;
  chatJid: string;
  onChange?: (tags: Tag[]) => void;
  compact?: boolean;
}

export const ChatTagsManager = ({ sessionId, chatJid, onChange, compact }: Props) => {
  const [current, setCurrent] = useState<Tag[]>([]);
  const [all, setAll] = useState<Tag[]>([]);
  const [loading, setLoading] = useState(false);
  const [open, setOpen] = useState(false);

  const refresh = async () => {
    if (!sessionId || !chatJid) return;
    setLoading(true);
    try {
      const [mine, every] = await Promise.all([
        listChatTags(sessionId, chatJid),
        listTags(),
      ]);
      setCurrent(mine);
      setAll(every);
      onChange?.(mine);
    } catch (e) {
      // ignore
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId, chatJid]);

  const selectedIds = useMemo(() => new Set(current.map((t) => t.id)), [current]);
  const available = all.filter((t) => !selectedIds.has(t.id));

  const attach = async (id: string) => {
    try {
      const next = await attachChatTag(sessionId, chatJid, id);
      setCurrent(next);
      onChange?.(next);
    } catch {
      // ignore
    }
  };
  const detach = async (id: string) => {
    try {
      await detachChatTag(sessionId, chatJid, id);
      const next = current.filter((t) => t.id !== id);
      setCurrent(next);
      onChange?.(next);
    } catch {
      // ignore
    }
  };

  return (
    <div className={compact ? "flex flex-wrap items-center gap-1" : "flex flex-wrap items-center gap-1.5"}>
      {current.map((t) => (
        <span
          key={t.id}
          className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-medium leading-none"
          style={tagChipStyle(t.color)}
        >
          <TagIcon className="h-2.5 w-2.5 opacity-80" />
          {t.name}
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              void detach(t.id);
            }}
            className="ml-0.5 grid h-3.5 w-3.5 place-items-center rounded-full hover:bg-black/10"
            title="Remover tag"
            aria-label={`Remover ${t.name}`}
          >
            <X className="h-2.5 w-2.5" />
          </button>
        </span>
      ))}
      <DropdownMenu open={open} onOpenChange={setOpen}>
        <DropdownMenuTrigger asChild>
          <Button
            size="sm"
            variant="outline"
            className="h-6 gap-1 rounded-full border-dashed px-2 text-[10px]"
            title="Adicionar tag"
          >
            <Plus className="h-3 w-3" />
            {compact && current.length > 0 ? "" : "Tag"}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-60 p-2">
          <div className="mb-1.5 text-[10px] uppercase tracking-wide text-muted-foreground">
            Disponíveis
          </div>
          {loading ? (
            <div className="px-1 py-2 text-xs text-muted-foreground">Carregando…</div>
          ) : available.length === 0 ? (
            <div className="px-1 py-2 text-xs text-muted-foreground">
              {all.length === 0 ? "Cadastre tags em /tags." : "Todas as tags já vinculadas."}
            </div>
          ) : (
            <ul className="max-h-56 space-y-0.5 overflow-y-auto">
              {available.map((t) => (
                <li key={t.id}>
                  <button
                    type="button"
                    onClick={() => {
                      void attach(t.id);
                    }}
                    className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs hover:bg-muted"
                  >
                    <span
                      className="inline-block h-3 w-3 rounded-full ring-1 ring-border"
                      style={{ backgroundColor: t.color }}
                    />
                    <span className="flex-1 truncate">{t.name}</span>
                    <Check className="h-3 w-3 opacity-0" />
                  </button>
                </li>
              ))}
            </ul>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
};