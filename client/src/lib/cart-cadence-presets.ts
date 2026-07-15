import type { CartCadenceStep } from "@/services/cartRecovery";

export interface CadencePreset {
  id: string;
  name: string;
  scenario: string; // ex.: "Carrinho abandonado", "Pagamento aprovado", "PIX pendente"
  description: string;
  steps: CartCadenceStep[];
}

/**
 * Modelos pré-prontos de réguas. As etapas são executadas em sequência,
 * com `delayMinutes` contado a partir do evento disparador (ex.: carrinho
 * abandonado, PIX gerado, compra aprovada).
 */
export const CADENCE_PRESETS: CadencePreset[] = [
  {
    id: "cart-abandoned-aggressive",
    name: "Carrinho abandonado — Agressiva (WhatsApp + Ligação + E-mail)",
    scenario: "Carrinho abandonado",
    description:
      "Sequência multi-canal para recuperar carrinhos abandonados em até 24h. Use quando o cliente sai do checkout sem pagar.",
    steps: [
      {
        channel: "whatsapp",
        delayMinutes: 15,
        template:
          "Olá {{name}}, tudo bem? 😊\nVi que você ia levar *{{product}}* mas não finalizou. Posso te ajudar a concluir?\n\n👉 {{checkout}}",
      },
      {
        channel: "whatsapp",
        delayMinutes: 60,
        template:
          "{{name}}, deixei separado o seu *{{product}}* ({{amount}}). Quer que eu envie por PIX, cartão ou boleto?\n\nLink rápido: {{checkout}}",
      },
      {
        channel: "call",
        delayMinutes: 180,
      },
      {
        channel: "email",
        delayMinutes: 360,
        subject: "{{name}}, seu pedido ainda está te esperando",
        template:
          "<p>Olá {{name}},</p><p>Notamos que você não concluiu a compra de <b>{{product}}</b>.</p><p>Para sua comodidade, deixamos o link ativo: <a href=\"{{checkout}}\">finalizar pedido</a>.</p><p>Qualquer dúvida estamos por aqui.</p>",
      },
      {
        channel: "whatsapp",
        delayMinutes: 1440,
        template:
          "Última chance, {{name}}! 🛒\nSeu *{{product}}* sai por {{amount}}. Posso liberar 10% off por mais 1h?\n\n{{checkout}}",
      },
    ],
  },
  {
    id: "cart-abandoned-soft",
    name: "Carrinho abandonado — Suave (só WhatsApp)",
    scenario: "Carrinho abandonado",
    description:
      "Versão menos invasiva, apenas WhatsApp, ideal para públicos sensíveis a contato.",
    steps: [
      {
        channel: "whatsapp",
        delayMinutes: 30,
        template:
          "Oi {{name}}! Vi que você se interessou por *{{product}}*. Posso tirar alguma dúvida?\n\n{{checkout}}",
      },
      {
        channel: "whatsapp",
        delayMinutes: 720,
        template:
          "{{name}}, ainda dá tempo de garantir seu *{{product}}* por {{amount}}. Link: {{checkout}}",
      },
    ],
  },
  {
    id: "pix-pending",
    name: "PIX pendente — Lembrete de pagamento",
    scenario: "PIX pendente",
    description:
      "Cliente gerou o PIX mas não pagou. Lembretes rápidos antes do vencimento (PIX expira em ~30 min na maioria dos gateways).",
    steps: [
      {
        channel: "whatsapp",
        delayMinutes: 5,
        template:
          "Olá {{name}}! Seu PIX de *{{amount}}* para *{{product}}* foi gerado e está aguardando pagamento.\n\nFinalize aqui: {{checkout}}\n\n⚠️ O código expira em alguns minutos.",
      },
      {
        channel: "whatsapp",
        delayMinutes: 15,
        template:
          "{{name}}, faltam poucos minutos pro seu PIX expirar ⏰\nPague agora e libere imediatamente seu *{{product}}*: {{checkout}}",
      },
      {
        channel: "whatsapp",
        delayMinutes: 60,
        template:
          "{{name}}, seu PIX expirou. Posso gerar um novo? Responda *SIM* que envio na hora. 💚",
      },
    ],
  },
  {
    id: "boleto-pending",
    name: "Boleto pendente — Lembrete antes do vencimento",
    scenario: "Boleto pendente",
    description:
      "Cliente gerou boleto mas ainda não pagou. Lembretes 1 dia antes e no dia do vencimento.",
    steps: [
      {
        channel: "email",
        delayMinutes: 60,
        subject: "Seu boleto de {{product}} já está disponível",
        template:
          "<p>Olá {{name}},</p><p>Recebemos seu pedido de <b>{{product}}</b> ({{amount}}).</p><p>O boleto está disponível em: <a href=\"{{checkout}}\">acessar boleto</a>.</p><p>Após o pagamento a confirmação leva até 2 dias úteis.</p>",
      },
      {
        channel: "whatsapp",
        delayMinutes: 1440,
        template:
          "Oi {{name}}! Lembrando que seu boleto de *{{product}}* ({{amount}}) vence amanhã.\nLink: {{checkout}}\n\nSe preferir pagar via PIX, me avise.",
      },
      {
        channel: "whatsapp",
        delayMinutes: 2880,
        template:
          "{{name}}, seu boleto vence *hoje*. Pague até o final do dia pra garantir seu *{{product}}*: {{checkout}}",
      },
    ],
  },
  {
    id: "payment-approved",
    name: "Pagamento aprovado — Boas-vindas e entrega",
    scenario: "Pagamento aprovado",
    description:
      "Cliente acabou de comprar. Confirma o pagamento, agradece e direciona para próximos passos (acesso, login, suporte).",
    steps: [
      {
        channel: "whatsapp",
        delayMinutes: 0,
        template:
          "✅ Pagamento aprovado, {{name}}!\nObrigado por adquirir *{{product}}*.\n\nEnviamos os detalhes de acesso para *{{email}}*. Qualquer dúvida estou por aqui!",
      },
      {
        channel: "email",
        delayMinutes: 1,
        subject: "Bem-vindo(a) ao {{product}}, {{name}}! 🎉",
        template:
          "<p>Olá {{name}},</p><p>Seu pagamento de <b>{{amount}}</b> foi confirmado. Seja muito bem-vindo(a)!</p><p>Acesse seu produto: <a href=\"{{checkout}}\">entrar agora</a></p><p>Precisando de ajuda, responda este e-mail.</p>",
      },
      {
        channel: "whatsapp",
        delayMinutes: 1440,
        template:
          "Oi {{name}}, tudo certo com seu *{{product}}*? Já conseguiu acessar? Se precisar de qualquer coisa, é só responder por aqui. 🙌",
      },
    ],
  },
  {
    id: "chargeback-prevent",
    name: "Pós-venda — Prevenção de chargeback",
    scenario: "Pós-venda",
    description:
      "Reforça o relacionamento nos primeiros 7 dias depois da compra para reduzir disputas e pedidos de reembolso.",
    steps: [
      {
        channel: "whatsapp",
        delayMinutes: 4320,
        template:
          "{{name}}, passando pra saber como tá sendo sua experiência com *{{product}}* 💬\nQualquer dúvida, fala comigo!",
      },
      {
        channel: "email",
        delayMinutes: 7200,
        subject: "Como está sendo sua experiência, {{name}}?",
        template:
          "<p>Olá {{name}},</p><p>Faz uma semana que você adquiriu <b>{{product}}</b>. Conta pra gente: como está sendo?</p><p>Se precisar de suporte, responda este e-mail.</p>",
      },
    ],
  },
  {
    id: "upsell-postsale",
    name: "Upsell pós-venda (oferta complementar)",
    scenario: "Upsell",
    description:
      "Oferece um produto complementar 3 dias depois da compra aprovada.",
    steps: [
      {
        channel: "whatsapp",
        delayMinutes: 4320,
        template:
          "Oi {{name}}! Como cliente do *{{product}}*, separei uma oferta especial pra você (somente hoje).\nDá uma olhada: {{checkout}}",
      },
      {
        channel: "email",
        delayMinutes: 5760,
        subject: "Oferta exclusiva para clientes {{product}}",
        template:
          "<p>{{name}}, preparamos uma oferta exclusiva pra quem já é cliente.</p><p><a href=\"{{checkout}}\">Ver oferta</a></p>",
      },
    ],
  },
];
