import { useEffect } from "react";
import { QRCodeSVG } from "qrcode.react";
import { Smartphone } from "lucide-react";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { useSessions } from "@/stores/sessions";
import { pairSession } from "@/services/sessions";
import { toast } from "sonner";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  sessionId: string;
  sessionName: string;
};

// Modal that displays the WhatsApp pairing QR code with the same wording and
// numbered instructions used by the reference design. The QR string is read
// reactively from the sessions store, so it refreshes as the broker rotates
// the code.
export const QRDialog = ({ open, onOpenChange, sessionId, sessionName }: Props) => {
  const qr = useSessions((s) => s.qrs[sessionId] ?? "");
  const paired = useSessions((s) =>
    s.sessions.find((x) => x.id === sessionId)?.paired ?? false,
  );

  // Auto-request a pairing as soon as the dialog opens.
  useEffect(() => {
    if (!open) return;
    pairSession(sessionId).catch((e) => toast.error((e as Error).message));
  }, [open, sessionId]);

  // Close the modal automatically once pairing succeeds.
  useEffect(() => {
    if (open && paired) onOpenChange(false);
  }, [open, paired, onOpenChange]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md gap-0 p-0">
        <div className="flex items-center justify-between border-b px-5 py-3.5">
          <div className="flex items-center gap-2 text-sm font-semibold">
            <Smartphone className="h-4 w-4 text-primary" />
            Conectar WhatsApp — {sessionName}
          </div>
        </div>

        <div className="space-y-5 px-5 py-5">
          <ol className="space-y-1.5 rounded-lg border bg-muted/30 p-4 text-sm leading-relaxed">
            <li>1. Abra o WhatsApp no seu celular</li>
            <li>
              2. Toque em <b>Menu</b> ou <b>Configurações</b>
            </li>
            <li>
              3. Toque em <b>Aparelhos conectados</b>
            </li>
            <li>4. Aponte seu celular para esta tela para escanear o código</li>
          </ol>

          <div className="grid place-items-center">
            <div className="grid h-[260px] w-[260px] place-items-center rounded-xl bg-white p-3 shadow-inner">
              {qr ? (
                <QRCodeSVG value={qr} size={232} includeMargin={false} />
              ) : (
                <div className="text-center text-xs text-muted-foreground">
                  Gerando QR Code…
                </div>
              )}
            </div>
            <p className="mt-3 text-center text-xs text-muted-foreground">
              O QR Code é atualizado automaticamente
            </p>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
};
