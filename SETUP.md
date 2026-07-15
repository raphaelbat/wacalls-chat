# Setup local (Windows / Linux / macOS)

Guia rápido pra rodar o projeto do zero. Você pode rodar tudo com **um único comando** (recomendado) ou em dois terminais separados.

## 1. Pré-requisitos

Instale uma vez por máquina:

- **Node.js 20 LTS** (inclui npm) — https://nodejs.org/
- **Go 1.22+** — https://go.dev/dl/
- **Git** — https://git-scm.com/

Confirme:

```bash
node -v   # v20.x.x
npm -v    # 10.x.x
go version
git --version
```

Se algum comando falhar, **feche e reabra o terminal** depois de instalar.

## 2. Clonar o projeto

```bash
git clone <URL_DO_SEU_REPO> "projeto call"
cd "projeto call"
```

## 3. Instalação (uma vez só)

Na **raiz** do projeto:

```bash
npm install          # instala concurrently na raiz
npm run setup        # instala deps do client + baixa módulos Go
```

## 4. Rodar tudo com um comando

Na raiz:

```bash
npm run dev
```

Isso sobe **frontend (5173) + backend (8080) juntos**, com logs coloridos prefixados `[CLIENT]` e `[SERVER]`. `Ctrl+C` derruba os dois.

Abra http://localhost:5173/ e faça login:

| Campo | Valor |
|-------|-------|
| Email | `admin@equipechat.com` |
| Senha | `adminpro` |

> Troque a senha após o primeiro login.

---

## Alternativa: dois terminais separados

Se preferir ver os logs em janelas separadas:

### Terminal 1 — Frontend (porta 5173)

```bash
cd client
npm install          # instala dependências (inclui Vite). Só na primeira vez ou após git pull.
npm run dev          # sobe Vite em http://localhost:5173
```

Erro `vite: command not found`? Significa que `npm install` não rodou nessa pasta. Rode `npm install` dentro de `client/` antes do `npm run dev`.

### Terminal 2 — Backend (porta 8080)

Abra **outro** terminal, na **raiz** do projeto (não em `client/`):

```bash
cd "projeto call"
go mod download      # baixa dependências Go. Só na primeira vez.
go run ./cmd/server  # sobe API em http://localhost:8080
```

Você verá:

```
default admin created email=admin@equipechat.com
HTTP server listening addr=:8080
```

## Atualizar depois de `git pull`

Sempre que puxar mudanças novas:

```bash
npm run setup
npm run dev
```

## Problemas comuns

| Erro | Causa | Solução |
|------|-------|---------|
| `vite: command not found` | Deps do client não instaladas | `npm run setup` na raiz |
| `concurrently: command not found` | Faltou `npm install` na raiz | Rode `npm install` na raiz |
| `[vite] http proxy error: /api/... ECONNREFUSED` | Backend não está rodando | Suba o Terminal 2 (`go run ./cmd/server`) |
| `proxy target localhost:3001` | `vite.config.ts` apontando pra porta errada | Edite `client/vite.config.ts` → `target: "http://127.0.0.1:8080"` |
| `go: command not found` | Go não instalado / PATH | Instale Go e reabra o terminal |
| Login não aceita admin padrão | Banco já tinha usuários | Pare o backend, apague `wacalls.db*` e suba de novo |

## Resetar o banco (zera usuários e sessões)

Pare o backend e:

```bash
# Windows PowerShell
del wacalls.db, wacalls.db-shm, wacalls.db-wal

# Linux / macOS
rm -f wacalls.db wacalls.db-shm wacalls.db-wal
```

Suba o backend de novo — o admin padrão é recriado automaticamente.