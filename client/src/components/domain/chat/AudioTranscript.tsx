import { useState } from "react";
import { FileText, Loader2 } from "lucide-react";
import { toast } from "sonner";

import type { ChatMessage } from "@/types/chat";
import { refKey, runTranscription, useTranscripts } from "@/stores/transcripts";

/**
 * Inline transcription control shown under audio bubbles. The text is stored
 * server-side so it stays searchable inside the conversation.
 */
export const AudioTranscript = ({ message, mine }: { message: ChatMessage; mine: boolean }) => {
  const key = refKey("message", message.id);
  const transcript = useTranscripts((s) => s.byRef[key]);
  const pending = useTranscripts((s) => !!s.pending[key]);
  const [open, setOpen] = useState(false);

  const run = async () => {
    try {
      const t = await runTranscription({
        sessionId: message.sessionId,
        scope: "message",
        refId: message.id,
        chatJid: message.chatJid,
        force: transcript?.status === "error",
      });
      setOpen(true);
      if (t.status === "empty") toast.info("Nenhuma fala reconhecida neste áudio.");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Falha ao transcrever o áudio");
    }
  };

  const tone = mine ? "text-primary-foreground/80" : "text-muted-foreground";
  const hasText = transcript?.status === "done" && !!transcript.text;

  return (
    <div className="mt-1">
      <button
        type="button"
        onClick={() => (hasText ? setOpen((v) => !v) : void run())}
        disabled={pending}
        className={`inline-flex items-center gap-1 text-[10px] font-medium underline-offset-2 hover:underline disabled:opacity-60 ${tone}`}
      >
        {pending ? <Loader2 className="h-3 w-3 animate-spin" /> : <FileText className="h-3 w-3" />}
        {pending
          ? "Transcrevendo…"
          : hasText
            ? open
              ? "Ocultar transcrição"
              : "Ver transcrição"
            : transcript?.status === "empty"
              ? "Sem fala reconhecida · tentar novamente"
              : transcript?.status === "error"
                ? "Erro na transcrição · tentar novamente"
                : "Transcrever áudio"}
      </button>
      {open && hasText && (
        <div
          className={`mt-1 whitespace-pre-wrap break-words rounded-md border px-2 py-1 text-[11px] leading-snug ${
            mine ? "border-primary-foreground/25 bg-primary-foreground/10" : "border-border bg-muted/40"
          }`}
        >
          {transcript!.text}
        </div>
      )}
    </div>
  );
};
