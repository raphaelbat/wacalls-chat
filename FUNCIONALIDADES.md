# Documentação de Funcionalidades - WACalls Chat

Esta lista descreve as principais funcionalidades implementadas no sistema WACalls Chat, organizadas por módulos.

---

## 1. Módulo de Chat e Atendimento
- **Chat Multicanal:** Interface de chat em tempo real para comunicação via WhatsApp.
- **Identificação de Chamadas:** Exibição do nome e foto do contato (sincronizados do WhatsApp) durante chamadas recebidas.
- **Saudação no pareamento:** ao conectar um número pelo QR Code, o sistema envia uma única mensagem na conversa do próprio número confirmando a conexão (texto configurável por `WACALLS_WELCOME_MESSAGE`, desligável com `WACALLS_WELCOME=off`).
- **Mensagens apagadas ocultas:** mensagens revogadas não aparecem na conversa nem na prévia da lista — a thread mostra só o que continua valendo.
- **Respostas Rápidas (/atalhos):** Snippets predefinidos com suporte a variáveis dinâmicas como `{{nome}}` e `{{protocolo}}`. Cada snippet pode ter **um anexo** (imagem, vídeo, áudio ou documento, até 32 MB): ao usar o atalho, a mídia é enviada com o texto como legenda.
- **Transcrição de Áudio:** Conversão automática de mensagens de voz e gravações de ligações em texto pesquisável.
- **Agendamento de Mensagens:** Permite programar o envio de mensagens para datas e horários específicos.
- **Follow-up Automático:** Lembretes configuráveis que são cancelados automaticamente se o cliente responder.
- **Notificações Sonoras:** Alertas sonoros para novos atendimentos em aguardando ou mensagens em grupos.

## 2. Kanban (Gestão de Fluxo)
- **Boards Customizáveis:** Criação de múltiplos quadros (ex: Vendas, Suporte, Atendimento).
- **Colunas e Cards:** Organização de atendimentos em colunas com movimentação por arraste (drag-and-drop).
- **Integração com Chat:** Possibilidade de vincular um atendimento diretamente a um card do Kanban pela tela de chat.
- **SLA por Coluna:** Definição de tempo limite (em horas) para permanência de um card em cada etapa, com alertas visuais de atraso.
- **Modelos Prontos:** Templates para criação rápida de novos quadros.

## 3. Sistema de Telefonia e VoIP
- **Discador Integrado:** Realização de chamadas diretamente pela plataforma utilizando números reais (E.164).
- **Gravação de Ligações:** Opção para gravar todas as chamadas e salvá-las automaticamente.
- **Relatório de Ligações:** Histórico completo com player de áudio integrado para ouvir as gravações.
- **Barra de Chamada Ativa:** Controle global persistente para silenciar (mute) ou desligar chamadas em andamento.

## 4. Gestão de Filas e Roteamento
- **Distribuição Automática (Round-Robin):** Roteamento inteligente de novos atendimentos entre os agentes disponíveis.
- **Limite de Atendimentos:** Controle de carga máxima por atendente para evitar sobrecarga.
- **Horário de Atendimento:** Configuração de expedientes por fila ou conexão com mensagens automáticas de "fora de horário".

## 5. Dashboard e Relatórios (SLA & Performance)
- **Métricas de SLA:** Acompanhamento de Tempo Médio de Primeira Resposta e Tempo Médio de Atendimento.
- **Ranking de Agentes:** Visualização de desempenho por atendente baseada em produtividade e avaliações.
- **Pesquisa de Satisfação (CSAT):** Coleta de notas ao encerramento dos atendimentos para análise de qualidade.

## 6. Administração e Segurança
- **Controle de Acesso:** Níveis de permissão para administradores e agentes.
- **Configuração SMTP:** Interface para gestão do servidor de e-mail (recuperação de senha).
- **Fluxo de Recuperação de Senha:** Sistema completo de "Esqueci minha senha" com validação e redefinição segura.
- **Instalador Automatizado:** Script para instalação local ou via Git, com rotinas de atualização simplificadas.

---
*Documento gerado em agosto de 2026.*
