import { useEffect, useRef, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ImagePlus, Trash2 } from "lucide-react";
import { updateContact } from "@/services/contacts";
import { toast } from "sonner";
import { toastError } from "@/lib/error-toast";

interface Props {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  sessionId: string;
  chatJid: string;
  currentName: string;
  currentAvatarUrl?: string;
  onSaved?: () => void;
}

export const EditContactDialog = ({
  open,
  onOpenChange,
  sessionId,
  chatJid,
  currentName,
  currentAvatarUrl,
  onSaved,
}: Props) => {
  const [name, setName] = useState(currentName);
  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState<string | undefined>(currentAvatarUrl);
  const [clearAvatar, setClearAvatar] = useState(false);
  const [saving, setSaving] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) {
      setName(currentName);
      setFile(null);
      setPreview(currentAvatarUrl);
      setClearAvatar(false);
    }
  }, [open, currentName, currentAvatarUrl]);

  const pickFile = (f: File | null) => {
    setFile(f);
    setClearAvatar(false);
    if (f) setPreview(URL.createObjectURL(f));
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      await updateContact(sessionId, chatJid, {
        name: name.trim(),
        avatar: file,
        clearAvatar,
      });
      toast.success("Contato atualizado");
      onSaved?.();
    } catch (e) {
      toastError(e, "Erro ao salvar contato");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Editar contato</DialogTitle>
          <DialogDescription>
            Atualize o nome exibido e a foto deste contato.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col items-center gap-3 py-2">
          <div className="relative">
            <div className="grid h-24 w-24 place-items-center overflow-hidden rounded-full bg-primary/10 text-2xl font-semibold text-primary ring-2 ring-background shadow">
              {!clearAvatar && preview ? (
                <img src={preview} alt={name} className="h-full w-full object-cover" />
              ) : (
                (name || "?").slice(0, 1).toUpperCase()
              )}
            </div>
            <button
              type="button"
              onClick={() => inputRef.current?.click()}
              className="absolute -bottom-1 -right-1 grid h-8 w-8 place-items-center rounded-full bg-primary text-primary-foreground shadow hover:opacity-90"
              title="Trocar foto"
            >
              <ImagePlus className="h-4 w-4" />
            </button>
          </div>
          <input
            ref={inputRef}
            type="file"
            accept="image/*"
            className="hidden"
            onChange={(e) => pickFile(e.target.files?.[0] ?? null)}
          />
          {(preview || file) && (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="text-xs text-muted-foreground"
              onClick={() => {
                setFile(null);
                setPreview(undefined);
                setClearAvatar(true);
              }}
            >
              <Trash2 className="mr-1 h-3.5 w-3.5" />
              Remover foto
            </Button>
          )}
        </div>

        <div className="space-y-2">
          <Label htmlFor="contact-name">Nome</Label>
          <Input
            id="contact-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Nome do contato"
          />
        </div>

        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            Cancelar
          </Button>
          <Button onClick={handleSave} disabled={saving || !name.trim()}>
            {saving ? "Salvando..." : "Salvar"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};