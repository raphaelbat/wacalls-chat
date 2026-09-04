<div align="center">

<img src="https://readme-typing-svg.demolab.com?font=Fira+Code&weight=700&size=32&duration=2800&pause=800&color=25D366&center=true&vCenter=true&width=700&lines=%F0%9F%93%9E+WaCalls;WhatsApp+multi-conex%C3%A3o+em+Go+%2B+React;Chat+%C2%B7+Filas+%C2%B7+Contatos+%C2%B7+Relat%C3%B3rios;100%25+Open+Source+%F0%9F%92%9A" alt="WaCalls" />

<br/>

# 📞 WaCalls

**Plataforma de atendimento WhatsApp multi-conexão em Go + React.**
Chat, filas, contatos, conexões multi-número e relatórios — tudo em um único binário.

<br/>

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?style=for-the-badge&logo=react&logoColor=black)](https://react.dev)
[![Vite](https://img.shields.io/badge/Vite-7-646CFF?style=for-the-badge&logo=vite&logoColor=white)](https://vitejs.dev)
[![Tailwind](https://img.shields.io/badge/Tailwind-3-06B6D4?style=for-the-badge&logo=tailwindcss&logoColor=white)](https://tailwindcss.com)

[![whatsmeow](https://img.shields.io/badge/whatsmeow-multi--device-25D366?style=for-the-badge&logo=whatsapp&logoColor=white)](https://github.com/tulir/whatsmeow)
[![SQLite](https://img.shields.io/badge/SQLite-embedded-003B57?style=for-the-badge&logo=sqlite&logoColor=white)](https://sqlite.org)
[![License](https://img.shields.io/badge/license-MIT-blue.svg?style=for-the-badge)](#-licença)
[![Open Source](https://img.shields.io/badge/open--source-%E2%9C%94-brightgreen?style=for-the-badge)](#-licença)

<br/>

![Status](https://img.shields.io/badge/status-stable-success?style=flat-square)
![Made with love](https://img.shields.io/badge/made%20with-%E2%9D%A4-red?style=flat-square)
![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen?style=flat-square)

<br/>

<img src="https://user-images.githubusercontent.com/74038190/212284100-561aa473-3905-4a80-b561-0d28506553ee.gif" width="900" alt="banner" />

</div>

---

> 💚 **Projeto open source.** Código-fonte completo, sem telemetria, sem dependência de serviços externos pagos. Rode em sua própria VPS.
>
> 💻 **Não quer VPS?** O [**WaCalls Local**](https://www.wacallslocal.com.br/) instala tudo no seu Windows em um clique — [veja os benefícios](#-wacalls-local--rodar-no-seu-pc-sem-vps-e-sem-servidor).

---

## ✨ O que esta versão inclui

Esta build entrega apenas as funcionalidades ativas no menu:

| Módulo | Rota | Descrição |
|---|---|---|
| 🔐 **Login** | `/login` | Autenticação por e-mail e senha. Admin padrão: `wacalls@admin.com` / `admin`. |
| 💬 **Chat** | `/chats` | Conversas em tempo real, envio de mídia, encerramento de atendimento (sem exigir motivo). |
| 👥 **Contatos** | `/contacts` | Lista, busca e edição de contatos sincronizados do WhatsApp. |
| 🗂️ **Filas** | `/queues` | Distribuição de atendimentos por fila/setor. |
| 📱 **Conexões** | `/connections` | Pareamento multi-número via QR Code, status e desconexão. |
| 📊 **Relatórios** | `/reports` | Mensagens enviadas/recebidas, chamadas, tickets, tendência diária. |
| 🛡️ **Usuários** | `/admin/users` | CRUD de usuários e perfis (apenas admin). |

### 🚫 O que **não** está incluso nesta versão

Para manter o deploy enxuto, foram removidos do menu (podem ser removidos também do `httpapi.go` se quiser build ainda menor):

- ❌ Construtor de fluxo (Flowbuilder)
- ❌ Kanban / Pipeline
- ❌ Campanhas / Broadcast em massa
- ❌ Agentes de IA
- ❌ Chamadas VoIP nativas (whatsmeow VoIP)
- ❌ TTS / gravação de chamadas
- ❌ Billing / Financeiro
- ❌ Cadastro público de novos usuários (só admin cria)
- ❌ Avaliação do atendimento pelo cliente

---

## 🧱 Stack

<div align="center">

<img src="https://skillicons.dev/icons?i=go,react,vite,tailwind,ts,sqlite,redis,linux,bash&theme=dark" alt="stack" />

</div>

- ⚙️ **Backend:** Go 1.26+, SQLite embarcado (pure-Go, sem CGO), Redis opcional
- 🎨 **Frontend:** React 19 + Vite 7 + Tailwind + shadcn/ui
- 📲 **WhatsApp:** [whatsmeow](https://github.com/tulir/whatsmeow) (multi-device)
- 🚀 **Deploy:** Binário único servindo API + frontend estático na mesma porta

---

## 💻 WaCalls Local — rodar no seu PC, sem VPS e sem servidor

O código deste repositório roda em qualquer lugar — inclusive no seu Windows, seguindo o
[SETUP.md](SETUP.md) (Go + Node + `npm run dev`). É de graça e continua sendo.

Para quem prefere **não mexer com terminal**, existe o **WaCalls Local**: um instalador
`.exe` que põe o sistema inteiro para rodar no seu computador em menos de um minuto.

<div align="center">

### 👉 [**Adquirir o WaCalls Local — wacallslocal.com.br**](https://www.wacallslocal.com.br/)

[![Comprar](https://img.shields.io/badge/comprar-instalador%20local-25D366?style=for-the-badge&logo=windows&logoColor=white)](https://www.wacallslocal.com.br/)
[![WhatsApp](https://img.shields.io/badge/suporte-81%2099588--5670-25D366?style=for-the-badge&logo=whatsapp&logoColor=white)](https://wa.me/5581995885670)

</div>

### Por que o instalador

| | |
|---|---|
| 🚫 **Sem VPS** | Nada de alugar servidor, apanhar de SSH ou levar susto com a fatura em dólar. Roda no PC que você já tem. |
| 🚫 **Sem domínio para começar** | Abre em `http://localhost:8080` no computador e pelo IP no celular, na mesma rede Wi-Fi. |
| ⚡ **Instala em menos de 1 minuto** | Um `.exe` com tudo dentro: programa, painel e serviço. Não precisa de Go, Node, Docker nem banco de dados. |
| 🔄 **Serviço do Windows** | Sobe junto com o computador e se reinicia sozinho. Você não abre nada para atender. |
| 🔐 **Seus dados no seu disco** | Conversas, mídias e contatos ficam na sua máquina, não na nuvem de ninguém. |
| 📱 **Multi-número** | Quantas conexões de WhatsApp você precisar — a licença é por computador, não por número. |
| 🤖 **Chatbot e campanhas** | Construtor de fluxo visual e disparo de mídia com rodízio entre números, prontos para usar. |
| 🌐 **Acesso externo opcional** | Tem domínio próprio? Um script publica `wacalls.suaempresa.com.br` com HTTPS por túnel da Cloudflare — sem abrir porta no roteador e sem IP fixo. |
| 🛟 **Suporte de gente** | WhatsApp direto com quem faz o sistema, e liberação da licença quando você troca de computador. |
| 🔁 **Atualizações incluídas** | Versão nova, mesmo instalador, sem refazer a instalação. |

> 💚 **O sistema continua gratuito e open source.** O que a assinatura cobre é a licença do
> instalador — a comodidade de instalar em um clique, as atualizações e o suporte.
> Quem quiser compilar e manter por conta própria tem tudo aqui neste repositório.

📖 Manual completo de instalação, ativação e acesso externo: [wacallslocal.com.br](https://www.wacallslocal.com.br/)
· Dúvidas: **81 99588-5670** · canal [Vem Fazer](https://youtube.com/@vemfazer)

---

## ⚡ Instalação rápida (VPS Linux)

Requisitos: **Go 1.26+**, **Node.js 20+**, **npm**, **unzip**, **systemd** (opcional).

### 1️⃣ Instalação direta pelo GitHub (recomendado)

> 📦 **Repositório oficial:** https://github.com/raphaelbat/wacalls-chat

Copie e cole **este único comando** na sua VPS. Ele baixa o instalador e abre o **menu interativo** com as opções `1) GIT · 2) ZIP · 3) Sair`:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/raphaelbat/wacalls-chat/main/client/scripts/instalador_wacalls.sh)
```

Você verá:

```
============================================================
        WaCalls — Instalador / Atualizador
============================================================
  1) Instalar / Atualizar via GIT  (padrão — recomendado)
  2) Instalar / Atualizar via ZIP  (arquivo local)
  3) Sair
============================================================
Escolha uma opção [1-3] (padrão 1):
```

Prefere pular o menu e ir direto? Use os atalhos:

```bash
# Direto via GIT (repo/branch padrão)
bash <(curl -fsSL https://raw.githubusercontent.com/raphaelbat/wacalls-chat/main/client/scripts/instalador_wacalls.sh) git

# Direto via GIT com fork/branch específico
bash <(curl -fsSL https://raw.githubusercontent.com/raphaelbat/wacalls-chat/main/client/scripts/instalador_wacalls.sh) \
  git https://github.com/SEU_USUARIO/wacalls-chat.git main

# Direto via ZIP local
bash <(curl -fsSL https://raw.githubusercontent.com/raphaelbat/wacalls-chat/main/client/scripts/instalador_wacalls.sh) \
  zip /root/wacalls.zip
```

### 2️⃣ Instalador via ZIP

Se preferir enviar o pacote manualmente:

```bash
# copie wacalls.zip para /root da VPS, então:
unzip -j /root/wacalls.zip 'wacalls/client/scripts/instalador_wacalls.sh' -d /root/
chmod +x /root/instalador_wacalls.sh
/root/instalador_wacalls.sh /root/wacalls.zip
```

### 3️⃣ Instalação manual

```bash
unzip wacalls.zip -d /opt/
cd /opt/wacalls

# Backend
go build -o wacalls ./cmd/server

# Frontend
cd client
npm install
npm run build   # gera client/dist (servido pelo binário)
cd ..

# Rodar
./wacalls -addr :8080 -db /var/lib/wacalls/wacalls.db
```

Na primeira execução o admin padrão `wacalls@admin.com` / `admin` é criado.

> ℹ️ O cadastro e a troca de senha **pelo painel** exigem 8+ caracteres. O admin padrão e os
> comandos de manutenção abaixo são exceção proposital: rodam localmente, por quem já tem
> acesso ao servidor, e aceitam senha curta. Se o painel ficar exposto na internet, troque
> essa senha.
Para trocar:

```bash
./wacalls -seed-admin-email meu@email.com -seed-admin-password minhaSenha
```

### 🛟 Manutenção (destravar o login)

```bash
# quais contas existem no banco?
./wacalls -db /var/lib/wacalls/wacalls.db -list-users

# cria (ou redefine a senha de) um admin, sem precisar do painel nem de e-mail
./wacalls -db /var/lib/wacalls/wacalls.db \
  -reset-admin-email meu@email.com -reset-admin-password minhaSenhaForte
```

Os dois comandos rodam e saem sem subir o servidor — pare o serviço antes de usá-los.

---

## 🔧 Configuração (`/root/wacalls/.env`)

```bash
# Banco (padrão SQLite; NÃO use mariadb — whatsmeow não suporta)
DB_DRIVER=sqlite

# Redis opcional — só necessário para múltiplas instâncias / cache compartilhado
REDIS_URL=redis://:senha@127.0.0.1:6379/0

# Saudação enviada para o próprio número assim que ele é pareado pelo QR Code
WACALLS_WELCOME=on                     # off para desligar
WACALLS_WELCOME_MESSAGE=Olá!\nTexto seu aqui.   # opcional; \n vira quebra de linha
```

### 👋 Saudação no pareamento

Assim que um número é conectado pelo QR Code, o sistema manda **uma** mensagem na conversa
do próprio número — serve de confirmação visual de que a conexão funcionou e de crédito de
quem distribui esta versão. O controle fica em `sessions.welcome_sent`, então reconectar ou
reiniciar o servidor **não** repete a mensagem; desconectar o número libera para o próximo
pareamento. Texto padrão em `cmd/server/welcome.go`, sobrescrevível pelo `.env`.

| Componente | SQLite (padrão) | SQLite + Redis |
|---|---|---|
| Empresas ativas | 30–80 | 150–220 |
| Sessões WhatsApp simultâneas | 50–150 | 300–500 |
| SSE fan-out | processo único | multi-instância |

---

## 🛠️ Systemd

```ini
[Unit]
Description=WaCalls
After=network.target

[Service]
WorkingDirectory=/opt/wacalls
EnvironmentFile=/opt/wacalls/.env
ExecStart=/opt/wacalls/wacalls -addr :8080 -db /var/lib/wacalls/wacalls.db
Restart=always
User=root

[Install]
WantedBy=multi-user.target
```

```bash
systemctl enable --now wacalls
journalctl -u wacalls -f
```

---

## 🌐 Endpoints ativos

- `POST /api/auth/login` · `GET /api/auth/me` · `POST /api/auth/logout`
- `GET/POST/PUT/DELETE /api/users` (admin)
- `GET/POST /api/sessions/*` — conexões WhatsApp (QR, status)
- `GET /api/sessions/{id}/chats/*` — chats e mensagens
- `GET /api/contacts` — contatos
- `GET/POST /api/queues` — filas de atendimento
- `GET/POST/PUT/DELETE /api/quick-replies` — respostas rápidas (texto + 1 anexo)
- `POST/DELETE /api/quick-replies/{id}/media` — anexa/remove a mídia do snippet
- `GET /api/reports/summary` — métricas do relatório
- `GET /api/events` (SSE) — eventos em tempo real

> ℹ️ Rotas dos módulos não usados (flows, kanban, campanhas, VoIP) continuam no código mas não são expostas pelo menu. Você pode removê-las editando `cmd/server/httpapi.go`.

---

## 📁 Estrutura

```
wacalls/
├── cmd/server/          # backend Go (HTTP + SSE + whatsmeow)
├── internal/            # domínio: wa, voip (legacy), auth
├── client/              # frontend React + Vite
│   ├── src/pages/       # Login, Chats, Connections, Contacts, Queues, Reports, AdminUsers
│   └── scripts/         # instalador_wacalls.sh
├── dist/                # build de frontend embutido (fallback)
├── go.mod / go.sum
├── README.md            # este arquivo
└── SETUP.md             # guia curto de deploy
```

---

## 🔄 Atualização

Rode o mesmo instalador com um novo `wacalls.zip`. Ele faz backup automático de `wacalls.db`, `.env`, `data/` e `media/` em `/root/wacalls-backup-<timestamp>`.

---


## 👥 Contributors

This project builds on the work of:

<div align="center">

<a href="https://github.com/jotadev66"><img src="https://github.com/jotadev66.png" width="70" height="70" style="border-radius:8px" alt="jotadev66" /></a>
<a href="https://github.com/jobasfernandes"><img src="https://github.com/jobasfernandes.png" width="70" height="70" style="border-radius:8px" alt="jobasfernandes" /></a>
<a href="https://github.com/edgardmessias"><img src="https://github.com/edgardmessias.png" width="70" height="70" style="border-radius:8px" alt="edgardmessias" /></a>
<a href="https://github.com/w3nder"><img src="https://github.com/w3nder.png" width="70" height="70" style="border-radius:8px" alt="w3nder" /></a>
<a href="https://github.com/raphaelbat"><img src="https://github.com/raphaelbat.png" width="70" height="70" style="border-radius:8px" alt="raphaelbat" /></a>

**[@jotadev66](https://github.com/jotadev66) · [@jobasfernandes](https://github.com/jobasfernandes) · [@edgardmessias](https://github.com/edgardmessias) · [@w3nder](https://github.com/w3nder) · [@raphaelbat](https://github.com/raphaelbat)**

</div>

---

## 🏆 Créditos

<div align="center">

Feito com muito 💚 e código aberto por:

</div>

- 👨‍💻 [**JotaDev66**](https://github.com/jotadev66) — mantenedor da lib base (WaCalls core)
- 👨‍💻 [**jobasfernandes**](https://github.com/jobasfernandes) — contribuidor
- 🚀 [**Raphaelbat**](https://github.com/raphaelbat) — desenvolvimento dos demais recursos do sistema (painel, chats, conexões, usuários, instalador e integrações)

<div align="center">

### 🎬 Um agradecimento especial ao canal:

[![YouTube](https://img.shields.io/badge/YOUTUBE-@vemfazer-FF0000?style=for-the-badge&logo=youtube&logoColor=white)](https://youtube.com/@vemfazer)

### 📺 Canal [Vem Fazer](https://youtube.com/@vemfazer)

🔥 **Se esse projeto te ajudou, dê uma força ao canal!** 🔥

[![Inscreva-se](https://img.shields.io/badge/%F0%9F%91%89_INSCREVA--SE_NO_CANAL-FF0000?style=for-the-badge&logo=youtube&logoColor=white)](https://youtube.com/@vemfazer?sub_confirmation=1)

*Curta, comente e compartilhe os vídeos — é isso que mantém o projeto vivo e gratuito!*

<br/>

<img src="https://user-images.githubusercontent.com/74038190/212284158-e840e285-664b-44d7-b79b-e264b5e54825.gif" width="400" alt="thanks" />

<br/>

**Feito com 💚 pela comunidade open source.**

</div>

---

<div align="center">

## 📜 Licença

**MIT** — código aberto, uso comercial permitido. Contribuições via PR são bem-vindas. 🚀

</div>



