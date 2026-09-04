/**
 * Escolha de quem recebe a campanha.
 *
 * Quatro caminhos, todos terminando na mesma coisa: uma lista de contatos que
 * é gravada como fotografia na campanha. A lista não se atualiza sozinha depois
 * — campanha que cresce sozinha dispara para quem você não pretendia.
 */

import { useEffect, useMemo, useRef, useState } from "react";
import { Check, Loader2, Search, Upload, Users } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { listContacts, type ContactRow } from "@/services/contacts";
import { listTags } from "@/services/tags";
import { chatsComTag } from "@/services/campaigns";
import type { Tag } from "@/types/tag";

export type Destinatario = { jid: string; name: string };

type Aba = "tag" | "lista" | "csv" | "recentes";

const ABAS: Array<{ id: Aba; label: string }> = [
  { id: "tag", label: "Por tag" },
  { id: "lista", label: "Escolher na lista" },
  { id: "csv", label: "Planilha" },
  { id: "recentes", label: "Conversas recentes" },
];

/** Grupos não entram em campanha: disparar em grupo é a via rápida da denúncia. */
const soPessoas = (c: ContactRow) => !c.isGroup && !!c.chatJid;

/**
 * Aceita telefone em qualquer formato e devolve o JID do WhatsApp.
 * Números brasileiros sem o 55 recebem o código do país.
 */
export const telefoneParaJid = (bruto: string): string => {
  const so = String(bruto ?? "").replace(/\D/g, "");
  if (so.length < 8) return "";
  const comPais = so.length <= 11 ? `55${so}` : so;
  return `${comPais}@s.whatsapp.net`;
};

export const AudiencePicker = ({
  onAdicionar,
  ocupado,
}: {
  onAdicionar: (destinatarios: Destinatario[]) => Promise<void> | void;
  ocupado?: boolean;
}) => {
  const [aba, setAba] = useState<Aba>("tag");

  // --- por tag -------------------------------------------------------------
  const [tags, setTags] = useState<Tag[]>([]);
  const [tagId, setTagId] = useState("");

  // --- lista / recentes ----------------------------------------------------
  const [contatos, setContatos] = useState<ContactRow[]>([]);
  const [busca, setBusca] = useState("");
  const [carregando, setCarregando] = useState(false);
  const [marcados, setMarcados] = useState<Set<string>>(new Set());
  const [dias, setDias] = useState(30);

  // --- csv -----------------------------------------------------------------
  const arquivoRef = useRef<HTMLInputElement>(null);
  const [previaCsv, setPreviaCsv] = useState<Destinatario[]>([]);

  useEffect(() => {
    void listTags().then(setTags).catch(() => {});
  }, []);

  useEffect(() => {
    if (aba !== "lista" && aba !== "recentes") return;
    setCarregando(true);
    void listContacts({ q: busca, kind: "user", limit: 200 })
      .then((r) => setContatos(r.contacts.filter(soPessoas)))
      .catch((e) => toast.error((e as Error).message))
      .finally(() => setCarregando(false));
  }, [aba, busca]);

  const recentes = useMemo(() => {
    const corte = Date.now() / 1000 - dias * 86400;
    return contatos.filter((c) => c.lastTs > corte);
  }, [contatos, dias]);

  const alternar = (jid: string) => {
    setMarcados((s) => {
      const n = new Set(s);
      if (n.has(jid)) n.delete(jid);
      else n.add(jid);
      return n;
    });
  };

  const lerCsv = async (file: File) => {
    const texto = await file.text();
    const linhas = texto.split(/\r?\n/).filter((l) => l.trim());
    const saida: Destinatario[] = [];
    for (const [i, linha] of linhas.entries()) {
      const colunas = linha.split(/[,;\t]/).map((c) => c.trim().replace(/^"|"$/g, ""));
      // Cabeçalho: se a primeira linha não tem número, é título de coluna.
      if (i === 0 && !colunas.some((c) => /\d{8,}/.test(c))) continue;
      const colTelefone = colunas.find((c) => /\d{8,}/.test(c)) ?? "";
      const jid = telefoneParaJid(colTelefone);
      if (!jid) continue;
      const nome = colunas.find((c) => c !== colTelefone && /[A-Za-zÀ-ú]/.test(c)) ?? "";
      saida.push({ jid, name: nome });
    }
    if (!saida.length) {
      toast.error("Não achei nenhum telefone válido nesse arquivo.");
      return;
    }
    setPreviaCsv(saida);
  };

  const adicionar = async (lista: Destinatario[]) => {
    if (!lista.length) {
      toast.error("Selecione ao menos um contato.");
      return;
    }
    await onAdicionar(lista);
    setMarcados(new Set());
    setPreviaCsv([]);
    if (arquivoRef.current) arquivoRef.current.value = "";
  };

  const adicionarPorTag = async () => {
    if (!tagId) {
      toast.error("Escolha uma tag.");
      return;
    }
    try {
      const chats = await chatsComTag(tagId);
      const nomePorJid = new Map(contatos.map((c) => [c.chatJid, c.name]));
      const lista = chats
        .filter((c) => !c.chatJid.includes("@g.us"))
        .map((c) => ({ jid: c.chatJid, name: nomePorJid.get(c.chatJid) ?? "" }));
      if (!lista.length) {
        toast.error("Nenhuma conversa tem essa tag.");
        return;
      }
      await adicionar(lista);
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  const listaVisivel = aba === "recentes" ? recentes : contatos;

  return (
    <div className="rounded-xl border">
      <div className="flex flex-wrap gap-1 border-b p-1">
        {ABAS.map((a) => (
          <button
            key={a.id}
            type="button"
            onClick={() => setAba(a.id)}
            className={`rounded-md px-3 py-1.5 text-xs font-medium transition ${
              aba === a.id ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-accent"
            }`}
          >
            {a.label}
          </button>
        ))}
      </div>

      <div className="space-y-3 p-3">
        {/* ------------------------------------------------------------ tag */}
        {aba === "tag" && (
          <>
            <Label className="text-xs">Tag</Label>
            <select
              value={tagId}
              onChange={(e) => setTagId(e.target.value)}
              className="h-10 w-full rounded-md border bg-background px-3 text-sm"
            >
              <option value="">— Escolha —</option>
              {tags.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
            <p className="text-[11px] leading-snug text-muted-foreground">
              Entram todos os contatos que <strong>hoje</strong> têm essa tag. Marcar mais alguém depois
              não muda esta campanha.
            </p>
            <Button size="sm" onClick={adicionarPorTag} disabled={ocupado || !tagId}>
              <Users className="mr-1.5 h-3.5 w-3.5" />
              Adicionar quem tem a tag
            </Button>
          </>
        )}

        {/* ------------------------------------------------- lista/recentes */}
        {(aba === "lista" || aba === "recentes") && (
          <>
            {aba === "recentes" ? (
              <div className="flex items-center gap-2">
                <Label className="whitespace-nowrap text-xs">Falaram nos últimos</Label>
                <Input
                  type="number"
                  min={1}
                  value={dias}
                  onChange={(e) => setDias(Number(e.target.value) || 1)}
                  className="h-9 w-20 text-sm"
                />
                <span className="text-xs text-muted-foreground">dias</span>
              </div>
            ) : (
              <div className="relative">
                <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={busca}
                  onChange={(e) => setBusca(e.target.value)}
                  placeholder="Buscar por nome ou número"
                  className="h-9 pl-8 text-sm"
                />
              </div>
            )}

            <div className="max-h-56 overflow-y-auto rounded-lg border">
              {carregando ? (
                <div className="grid place-items-center py-8">
                  <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                </div>
              ) : !listaVisivel.length ? (
                <p className="py-8 text-center text-xs text-muted-foreground">Nenhum contato aqui.</p>
              ) : (
                listaVisivel.map((c) => (
                  <button
                    key={c.chatJid}
                    type="button"
                    onClick={() => alternar(c.chatJid)}
                    className="flex w-full items-center gap-2 border-b px-3 py-2 text-left last:border-b-0 hover:bg-accent"
                  >
                    <span
                      className={`grid h-4 w-4 shrink-0 place-items-center rounded border ${
                        marcados.has(c.chatJid) ? "border-primary bg-primary text-primary-foreground" : ""
                      }`}
                    >
                      {marcados.has(c.chatJid) ? <Check className="h-3 w-3" /> : null}
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-xs font-medium">{c.name || c.phone}</span>
                      <span className="block truncate text-[10px] text-muted-foreground">{c.phone}</span>
                    </span>
                  </button>
                ))
              )}
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <Button
                size="sm"
                onClick={() =>
                  adicionar(
                    listaVisivel
                      .filter((c) => marcados.has(c.chatJid))
                      .map((c) => ({ jid: c.chatJid, name: c.name })),
                  )
                }
                disabled={ocupado || !marcados.size}
              >
                Adicionar {marcados.size || ""} selecionado{marcados.size === 1 ? "" : "s"}
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setMarcados(new Set(listaVisivel.map((c) => c.chatJid)))}
                disabled={!listaVisivel.length}
              >
                Marcar todos ({listaVisivel.length})
              </Button>
              {marcados.size ? (
                <Button variant="ghost" size="sm" onClick={() => setMarcados(new Set())}>
                  Limpar
                </Button>
              ) : null}
            </div>
          </>
        )}

        {/* ------------------------------------------------------------ csv */}
        {aba === "csv" && (
          <>
            <input
              ref={arquivoRef}
              type="file"
              accept=".csv,.txt,text/csv"
              className="hidden"
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (f) void lerCsv(f);
              }}
            />
            <Button variant="outline" size="sm" onClick={() => arquivoRef.current?.click()}>
              <Upload className="mr-1.5 h-3.5 w-3.5" />
              Escolher arquivo
            </Button>
            <p className="text-[11px] leading-snug text-muted-foreground">
              Uma linha por contato, com telefone e nome separados por vírgula ou ponto e vírgula.
              Cabeçalho é reconhecido sozinho. Número sem o 55 recebe o código do Brasil.
            </p>
            {previaCsv.length ? (
              <div className="space-y-2 rounded-lg border bg-muted/20 p-2.5">
                <p className="text-xs">
                  <strong>{previaCsv.length}</strong> contato{previaCsv.length === 1 ? "" : "s"} lido
                  {previaCsv.length === 1 ? "" : "s"}. Primeiros:
                </p>
                <ul className="space-y-0.5 font-mono text-[10px] text-muted-foreground">
                  {previaCsv.slice(0, 4).map((d, i) => (
                    <li key={i} className="truncate">
                      {d.jid.replace("@s.whatsapp.net", "")} {d.name ? `· ${d.name}` : ""}
                    </li>
                  ))}
                </ul>
                <Button size="sm" onClick={() => adicionar(previaCsv)} disabled={ocupado}>
                  Adicionar os {previaCsv.length}
                </Button>
              </div>
            ) : null}
          </>
        )}
      </div>
    </div>
  );
};
