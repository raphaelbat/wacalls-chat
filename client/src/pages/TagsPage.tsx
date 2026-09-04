import { useCallback, useEffect, useState } from "react";
import { Loader2, Plus, Tag as TagIcon, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import { createTag, deleteTag, listTags, updateTag } from "@/services/tags";
import type { Tag } from "@/types/tag";

/**
 * Tags: criar, renomear, trocar a cor e apagar.
 *
 * Antes só dava para usar tag já existente dentro da conversa — não havia
 * onde criá-las nem corrigir a cor de uma. Esta tela é o lugar disso.
 */

// Cores prontas para não obrigar ninguém a pensar em hexadecimal. São as mesmas
// famílias usadas no resto do painel, para a tag não destoar da conversa.
const CORES = [
  "#25D366", "#0F7A41", "#2F80ED", "#1C4F8A",
  "#F0B357", "#E8796A", "#B5179E", "#7048E8",
  "#00B8D9", "#6B7D91",
];

export default function TagsPage() {
  const { t } = useTranslation();
  const [tags, setTags] = useState<Tag[]>([]);
  const [carregando, setCarregando] = useState(true);
  const [nome, setNome] = useState("");
  const [cor, setCor] = useState(CORES[0]);
  const [salvando, setSalvando] = useState(false);
  const [apagando, setApagando] = useState<Tag | null>(null);

  const carregar = useCallback(async () => {
    setCarregando(true);
    try {
      setTags(await listTags());
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setCarregando(false);
    }
  }, []);

  useEffect(() => {
    void carregar();
  }, [carregar]);

  const criar = async () => {
    const limpo = nome.trim();
    if (!limpo) return;
    if (tags.some((x) => x.name.toLowerCase() === limpo.toLowerCase())) {
      toast.error(t("pages.tags.duplicate", { defaultValue: "Já existe uma tag com esse nome." }));
      return;
    }
    setSalvando(true);
    try {
      await createTag(limpo, cor);
      setNome("");
      await carregar();
      toast.success(t("pages.tags.created", { defaultValue: "Tag criada" }));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setSalvando(false);
    }
  };

  // Renomear e trocar cor salvam direto, sem botão: a lista é curta e o
  // caminho de "editar → salvar → fechar" só atrapalharia.
  const salvar = async (tag: Tag, campos: { name?: string; color?: string }) => {
    const nomeNovo = (campos.name ?? tag.name).trim();
    const corNova = campos.color ?? tag.color;
    if (!nomeNovo || (nomeNovo === tag.name && corNova === tag.color)) return;
    setTags((atual) => atual.map((x) => (x.id === tag.id ? { ...x, name: nomeNovo, color: corNova } : x)));
    try {
      await updateTag(tag.id, nomeNovo, corNova);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
      void carregar();
    }
  };

  const confirmarApagar = async () => {
    if (!apagando) return;
    try {
      await deleteTag(apagando.id);
      setApagando(null);
      await carregar();
      toast.success(t("pages.tags.deleted", { defaultValue: "Tag apagada" }));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <AppShell>
      <div className="mx-auto max-w-3xl space-y-6">
        <div>
          <h2 className="text-xl font-semibold tracking-tight">
            {t("pages.tags.title", { defaultValue: "Tags" })}
          </h2>
          <p className="text-sm text-muted-foreground">
            {t("pages.tags.subtitle", {
              defaultValue:
                "Etiquetas coloridas para marcar conversas. As tags criadas aqui aparecem no atendimento e nas campanhas.",
            })}
          </p>
        </div>

        {/* nova tag */}
        <div className="rounded-lg border p-4">
          <div className="flex flex-wrap items-end gap-3">
            <div className="min-w-[220px] flex-1">
              <Label htmlFor="tag-nome">{t("pages.tags.name", { defaultValue: "Nome da tag" })}</Label>
              <Input
                id="tag-nome"
                value={nome}
                maxLength={40}
                placeholder={t("pages.tags.placeholder", { defaultValue: "Ex.: Orçamento, VIP, Sem retorno" })}
                onChange={(e) => setNome(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && void criar()}
              />
            </div>
            <Button onClick={() => void criar()} disabled={salvando || !nome.trim()}>
              {salvando ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : <Plus className="mr-1 h-4 w-4" />}
              {t("pages.tags.create", { defaultValue: "Criar tag" })}
            </Button>
          </div>
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <span className="text-xs text-muted-foreground">{t("pages.tags.color", { defaultValue: "Cor" })}:</span>
            {CORES.map((c) => (
              <button
                key={c}
                type="button"
                aria-label={c}
                onClick={() => setCor(c)}
                className={`h-6 w-6 rounded-full border-2 transition ${cor === c ? "border-foreground" : "border-transparent"}`}
                style={{ background: c }}
              />
            ))}
          </div>
        </div>

        {/* lista */}
        <div className="rounded-lg border">
          {carregando ? (
            <p className="px-4 py-8 text-center text-sm text-muted-foreground">
              <Loader2 className="mr-2 inline h-4 w-4 animate-spin" />
              {t("common.loading", { defaultValue: "Carregando…" })}
            </p>
          ) : tags.length === 0 ? (
            <div className="px-4 py-10 text-center">
              <TagIcon className="mx-auto mb-2 h-6 w-6 text-muted-foreground" />
              <p className="text-sm text-muted-foreground">
                {t("pages.tags.empty", { defaultValue: "Nenhuma tag ainda. Crie a primeira acima." })}
              </p>
            </div>
          ) : (
            <ul className="divide-y">
              {tags.map((tag) => (
                <li key={tag.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                  <span className="h-3 w-3 shrink-0 rounded-full" style={{ background: tag.color }} />
                  <Input
                    defaultValue={tag.name}
                    maxLength={40}
                    className="h-8 w-48"
                    onBlur={(e) => void salvar(tag, { name: e.target.value })}
                    onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
                  />
                  <div className="flex flex-wrap items-center gap-1.5">
                    {CORES.map((c) => (
                      <button
                        key={c}
                        type="button"
                        aria-label={c}
                        onClick={() => void salvar(tag, { color: c })}
                        className={`h-5 w-5 rounded-full border-2 transition ${tag.color === c ? "border-foreground" : "border-transparent"}`}
                        style={{ background: c }}
                      />
                    ))}
                  </div>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="ml-auto text-destructive"
                    onClick={() => setApagando(tag)}
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </div>

        <ConfirmDialog
          open={!!apagando}
          onOpenChange={(v) => !v && setApagando(null)}
          title={t("pages.tags.confirmTitle", { defaultValue: "Apagar esta tag?" })}
          description={t("pages.tags.confirmBody", {
            defaultValue: "Ela sai de todas as conversas que estavam marcadas com ela. As conversas continuam.",
          })}
          confirmLabel={t("common.delete", { defaultValue: "Apagar" })}
          destructive
          onConfirm={confirmarApagar}
        />
      </div>
    </AppShell>
  );
}
