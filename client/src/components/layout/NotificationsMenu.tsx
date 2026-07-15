import { useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Bell, CheckCheck, MessageSquare, PhoneIncoming, Users } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Button } from "@/components/ui/button";
import { useChats, markChatAsRead, setActiveChat } from "@/stores/chats";
import { useCalls } from "@/stores/calls";
import { formatPhone } from "@/lib/phone-format";

// NotificationsMenu surfaces a single bell with the combined unread/incoming
// count and a popover listing the most recent unread chats plus any incoming
// call. The data already lives in the chats/calls stores, so this component is
// purely derived state — no extra fetches.
export const NotificationsMenu = () => {
  const chatsBySession = useChats((s) => s.chatsBySession);
  const incoming = useCalls((s) => s.incoming);
  const navigate = useNavigate();

  const { unreadChats, totalUnread } = useMemo(() => {
    const items: Array<{
      sessionId: string;
      jid: string;
      name: string;
      unread: number;
      lastMessage: string;
      avatarUrl?: string;
      isGroup?: boolean;
    }> = [];
    let total = 0;
    for (const [sessionId, chats] of Object.entries(chatsBySession)) {
      for (const c of chats) {
        const unread = c.unread ?? 0;
        if (unread <= 0) continue;
        total += 1;
        items.push({
          sessionId,
          jid: c.chatJid,
          name: c.name || c.chatJid,
          unread,
          lastMessage: c.lastMessage,
          avatarUrl: c.avatarUrl,
          isGroup: !!c.isGroup,
        });
      }
    }
    items.sort((a, b) => b.unread - a.unread);
    return { unreadChats: items.slice(0, 8), totalUnread: total };
  }, [chatsBySession]);

  const incomingCount = incoming ? 1 : 0;
  const badgeCount = totalUnread + incomingCount;

  // Zera os contadores de não lidas localmente — o servidor reenvia o estado
  // real via SSE quando novas mensagens chegarem.
  const clearAll = () => {
    for (const c of unreadChats) markChatAsRead(c.sessionId, c.jid);
    for (const [sid, list] of Object.entries(chatsBySession)) {
      for (const c of list) if ((c.unread ?? 0) > 0) markChatAsRead(sid, c.chatJid);
    }
  };

  // Abre a conversa: navega para /chats com sid/jid e já marca como lida.
  // Se o chat estiver "em atendimento" (status open), o ChatsPage troca a aba
  // automaticamente via efeito de status, então sempre cai no atendimento certo.
  const openChat = (sessionId: string, jid: string) => {
    setActiveChat(sessionId, jid);
    markChatAsRead(sessionId, jid);
    navigate(`/chats?sid=${encodeURIComponent(sessionId)}&jid=${encodeURIComponent(jid)}`);
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className="relative" aria-label="Notificações">
          <Bell className="h-4 w-4" />
          {badgeCount > 0 && (
            <span className="absolute -right-0.5 -top-0.5 grid h-4 min-w-[1rem] place-items-center rounded-full bg-destructive px-1 text-[10px] font-semibold leading-none text-destructive-foreground">
              {badgeCount > 99 ? "99+" : badgeCount}
            </span>
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-80">
        <DropdownMenuLabel className="flex items-center justify-between gap-2">
          <span>Notificações</span>
          <div className="flex items-center gap-1">
            {badgeCount > 0 && (
              <span className="rounded-full bg-muted px-2 py-0.5 text-[10px] font-medium text-muted-foreground">
                {badgeCount} {badgeCount === 1 ? "nova" : "novas"}
              </span>
            )}
            {totalUnread > 0 && (
              <Button
                variant="ghost"
                size="sm"
                className="h-6 px-2 text-[11px]"
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  clearAll();
                }}
                title="Limpar notificações"
              >
                <CheckCheck className="mr-1 h-3 w-3" />
                Limpar
              </Button>
            )}
          </div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {incoming && (
          <>
            <DropdownMenuItem asChild>
              <Link to="/" className="flex items-start gap-2">
                <span className="mt-0.5 grid h-7 w-7 shrink-0 place-items-center rounded-full bg-emerald-500/15 text-emerald-500">
                  <PhoneIncoming className="h-3.5 w-3.5" />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium">Chamada recebida</span>
                  <span className="block truncate text-xs text-muted-foreground">{formatPhone(incoming.peer)}</span>
                </span>
              </Link>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
          </>
        )}
        {unreadChats.length === 0 && !incoming ? (
          <div className="px-3 py-6 text-center text-xs text-muted-foreground">
            Nenhuma notificação por aqui.
          </div>
        ) : (
          unreadChats.map((c) => (
            <DropdownMenuItem
              key={`${c.sessionId}:${c.jid}`}
              onSelect={(e) => {
                e.preventDefault();
                openChat(c.sessionId, c.jid);
              }}
              className="flex items-start gap-2"
            >
                <ChatAvatar
                  name={c.name}
                  avatarUrl={c.avatarUrl}
                  isGroup={c.isGroup}
                />
                <span className="min-w-0 flex-1">
                  <span className="flex items-center justify-between gap-2">
                    <span className="truncate text-sm font-medium">{c.name}</span>
                    <span className="shrink-0 rounded-full bg-primary px-1.5 text-[10px] font-semibold text-primary-foreground">
                      {c.unread > 99 ? "99+" : c.unread}
                    </span>
                  </span>
                  <span className="block truncate text-xs text-muted-foreground">
                    {c.lastMessage || "Nova mensagem"}
                  </span>
                </span>
            </DropdownMenuItem>
          ))
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
};

// Renders the contact/group photo with graceful fallbacks: initials when no
// image is set, a group icon when the chat is a community/group, and the
// generic message icon as a last resort.
const ChatAvatar = ({
  name,
  avatarUrl,
  isGroup,
}: {
  name: string;
  avatarUrl?: string;
  isGroup?: boolean;
}) => {
  const initials = name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase() ?? "")
    .join("");

  return (
    <span className="relative mt-0.5 grid h-8 w-8 shrink-0 place-items-center overflow-hidden rounded-full bg-primary/15 text-primary">
      {avatarUrl ? (
        <img
          src={avatarUrl}
          alt={name}
          className="h-full w-full object-cover"
          onError={(e) => {
            (e.currentTarget as HTMLImageElement).style.display = "none";
          }}
        />
      ) : initials ? (
        <span className="text-[11px] font-semibold">{initials}</span>
      ) : isGroup ? (
        <Users className="h-3.5 w-3.5" />
      ) : (
        <MessageSquare className="h-3.5 w-3.5" />
      )}
    </span>
  );
};