import { Phone } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useDialerUI } from "@/stores/dialerUI";
import { useAuth } from "@/stores/auth";
import { hasPermission } from "@/lib/permissions";

// Header trigger placed next to the notifications bell. Keeps the dialer one
// click away from anywhere in the app without occupying a sidebar slot.
export const DialerTriggerButton = () => {
  const user = useAuth((s) => s.user);
  const open = useDialerUI((s) => s.openDialer);
  if (!user || !hasPermission(user, "dialer")) return null;
  return (
    <Button
      variant="ghost"
      size="icon"
      onClick={() => open()}
      aria-label="Abrir discador"
      title="Discador"
      className="relative text-primary hover:bg-primary/10 hover:text-primary"
    >
      <Phone className="h-5 w-5" />
    </Button>
  );
};