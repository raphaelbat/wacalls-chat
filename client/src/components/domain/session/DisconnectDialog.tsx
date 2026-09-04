import { AlertTriangle } from "lucide-react";
import { toast } from "sonner";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { logoutSession } from "@/services/sessions";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  sessionId: string;
  sessionName: string;
};

// Confirmation dialog matching the reference: a triangle icon, body copy that
// names the instance in bold, and a destructive "Desconectar" action.
export const DisconnectDialog = ({ open, onOpenChange, sessionId, sessionName }: Props) => {
  const onConfirm = async () => {
    try {
      await logoutSession(sessionId);
      toast.success("Instância desconectada");
      onOpenChange(false);
    } catch (e) {
      toast.error((e as Error).message);
    }
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md gap-0 p-0">
        <div className="flex items-center justify-between border-b px-5 py-3.5">
          <div className="flex items-center gap-2 text-sm font-semibold">
            <AlertTriangle className="h-4 w-4 text-amber-500" />
            Desconectar instância
          </div>
        </div>
        <div className="px-5 py-4 text-sm leading-relaxed text-foreground/80">
          A instância <b className="text-foreground">{sessionName}</b> será
          desconectada do WhatsApp. Para utilizá-la novamente, será necessário
          escanear um novo QR Code.
        </div>
        <div className="flex justify-end gap-2 border-t bg-muted/20 px-5 py-3">
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
            Cancelar
          </Button>
          <Button
            size="sm"
            className="bg-amber-500 text-white hover:bg-amber-500/90"
            onClick={onConfirm}
          >
            Desconectar
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
};
