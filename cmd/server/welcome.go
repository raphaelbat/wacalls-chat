package main

import (
	"context"
	"os"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// defaultWelcomeMessage e a saudacao enviada para o proprio numero assim que
// ele e pareado pelo QR Code. Serve como "recibo" de que a conexao funcionou e
// como credito de quem distribui esta versao.
//
// Personalize sem recompilar com a variavel WACALLS_WELCOME_MESSAGE, ou
// desligue com WACALLS_WELCOME=off.
const defaultWelcomeMessage = "✅ *WhatsApp conectado com sucesso!*\n" +
	"\n" +
	"Você está usando o *WaCalls*, distribuído pelo canal *Vem Fazer* " +
	"(youtube.com/@vemfazer) em parceria com o *EquipeChat*.\n" +
	"\n" +
	"💚 *O sistema é gratuito e open source* — pode usar à vontade.\n" +
	"\n" +
	"💻 *O instalador local para Windows é o serviço pago: R$ 39,90/mês*\n" +
	"Com ele o WaCalls roda no seu próprio computador e você *não paga VPS " +
	"nem domínio* — economia que já cobre a mensalidade no primeiro mês. " +
	"Instalação em poucos cliques, atualização e suporte inclusos.\n" +
	"\n" +
	"🤖 *Automação de WhatsApp sob medida*\n" +
	"Fale com a gente: *81 99588-5670*\n" +
	"\n" +
	"⭐ *Precisa de mais recursos?*\n" +
	"A versão completa — mais conexões, campanhas, chamadas e IA — está em " +
	"*vozzap.com.br*\n" +
	"\n" +
	"_Mensagem automática, enviada uma única vez, quando este número foi conectado._"

// welcomeEnabled permite desligar a saudacao pelo .env.
func welcomeEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WACALLS_WELCOME"))) {
	case "off", "0", "false", "no", "nao", "não":
		return false
	}
	return true
}

// welcomeMessageText devolve o texto configurado ou o padrao. Na variavel de
// ambiente, "\n" literal vira quebra de linha (facilita escrever no .env).
func welcomeMessageText() string {
	if v := strings.TrimSpace(os.Getenv("WACALLS_WELCOME_MESSAGE")); v != "" {
		return strings.ReplaceAll(v, `\n`, "\n")
	}
	return defaultWelcomeMessage
}

// sendWelcomeToSelf envia a saudacao para a conversa do proprio numero, uma
// unica vez por pareamento. O controle fica na coluna sessions.welcome_sent,
// entao reconexao e restart do servidor nao repetem a mensagem.
func (s *Session) sendWelcomeToSelf() {
	if s == nil || s.client == nil || s.mgr == nil || s.mgr.store == nil {
		return
	}
	if !welcomeEnabled() {
		return
	}
	id := s.client.Store.ID
	if id == nil {
		return
	}
	ctx := s.mgr.appCtx
	if ctx == nil {
		ctx = context.Background()
	}

	sent, err := s.mgr.store.welcomeSent(ctx, s.id)
	if err != nil || sent {
		return
	}
	// Marca antes de enviar: se o envio falhar, liberamos de novo logo abaixo.
	// Assim uma reconexao no meio do caminho nao dispara duas mensagens.
	if err := s.mgr.store.setWelcomeSent(ctx, s.id, true); err != nil {
		return
	}

	self := id.ToNonAD()
	text := welcomeMessageText()
	sendCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	resp, err := s.client.SendMessage(sendCtx, self, &waE2E.Message{Conversation: proto.String(text)})
	if err != nil {
		s.log.Warn("nao consegui enviar a saudacao de boas-vindas", "err", err)
		_ = s.mgr.store.setWelcomeSent(ctx, s.id, false)
		return
	}

	row := MessageRow{
		ID:        string(resp.ID),
		SessionID: s.id,
		ChatJID:   self.String(),
		SenderJID: self.String(),
		FromMe:    true,
		Ts:        resp.Timestamp.UnixMilli(),
		Kind:      "text",
		Body:      text,
	}
	if row.Ts == 0 {
		row.Ts = time.Now().UnixMilli()
	}
	if s.mgr.messages != nil {
		_ = s.mgr.messages.Insert(ctx, row)
	}
	if s.mgr.broker != nil {
		s.mgr.broker.emitMessage(row)
	}
	s.log.Info("saudacao de boas-vindas enviada", "to", self.String())
}
