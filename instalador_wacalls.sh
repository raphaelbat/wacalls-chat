#!/bin/bash
# =====================================================================
#  WaCalls (Go) — Instalador para Ubuntu 22.04 LTS
#  Adaptado a partir do instalador Equipechat v7.0 (Raphael Batista)
#  Projeto: https://github.com/  (WaCalls — chamadas WhatsApp em Go)
# =====================================================================

set -o pipefail

GREEN='\033[1;32m'
BLUE='\033[1;34m'
WHITE='\033[1;37m'
RED='\033[1;31m'
YELLOW='\033[1;33m'
CYAN='\033[1;36m'
MAGENTA='\033[1;35m'

# -------------------- Variáveis padrão --------------------
ARQUIVO_VARIAVEIS="VARIAVEIS_WACALLS"
ARQUIVO_ETAPAS="ETAPA_WACALLS"

DEPLOY_USER="deploy"
APP_NAME="wacalls"
APP_USER="root"
APP_HOME="/root"
APP_DIR="${APP_HOME}/${APP_NAME}"
REPO_PADRAO="https://github.com/raphaelbat/wacalls-chat.git"
BRANCH_PADRAO="feat/video-calls"
GO_VERSION_FALLBACK="1.26.4"
NODE_MAJOR="22"

ip_atual=$(curl -s --max-time 5 http://checkip.amazonaws.com || echo "desconhecido")

# -------------------- Utilidades --------------------
# Detecta uma porta TCP livre começando em $1 (padrão 8080).
detectar_porta_livre() {
  local p="${1:-8080}"
  local max=$((p + 200))
  while [ "$p" -lt "$max" ]; do
    if ! ss -ltn 2>/dev/null | awk '{print $4}' | grep -Eq "[:.]${p}$"; then
      echo "$p"
      return 0
    fi
    p=$((p + 1))
  done
  echo "${1:-8080}"
}

garantir_env_systemd() {
  local unit="/etc/systemd/system/${APP_NAME}.service"
  local key="$1"
  local value="$2"
  [ -f "${unit}" ] || return 0
  if grep -qE "^Environment=${key}=" "${unit}"; then
    sed -i -E "s|^Environment=${key}=.*|Environment=${key}=${value}|" "${unit}" || true
  else
    sed -i "/^\[Service\]/a Environment=${key}=${value}" "${unit}" || true
  fi
}

garantir_static_dist() {
  local app_dir="${1:-$APP_DIR}"
  local frontend_dir="${2:-}"
  local unit="/etc/systemd/system/${APP_NAME}.service"
  local target="${app_dir}/dist"

  # Candidatos onde o build pode ter sido gerado, dependendo do outDir do vite:
  #  - ${app_dir}/dist              (padrão esperado)
  #  - ${app_dir}/client/dist       (layout monorepo antigo)
  #  - ${frontend_dir}/dist         (frontend dentro de subpasta)
  #  - ${frontend_dir}/../dist      (vite config com outDir: '../dist')
  #  - ${app_dir}/../dist           (idem, quando app_dir == frontend_dir)
  local candidates=(
    "${app_dir}/dist"
    "${app_dir}/client/dist"
  )
  if [ -n "${frontend_dir}" ]; then
    candidates+=( "${frontend_dir}/dist" "$(cd "${frontend_dir}/.." 2>/dev/null && pwd)/dist" )
  fi
  candidates+=( "$(cd "${app_dir}/.." 2>/dev/null && pwd)/dist" )

  if [ ! -f "${target}/index.html" ]; then
    local c
    for c in "${candidates[@]}"; do
      [ -z "${c}" ] && continue
      [ "${c}" = "${target}" ] && continue
      if [ -f "${c}/index.html" ]; then
        log_info "Espelhando ${c} -> ${target}"
        rm -rf "${target}"
        cp -a "${c}" "${target}" || true
        # Se o origem está fora de app_dir, remove para não acumular lixo
        case "${c}" in
          "${app_dir}/"*) : ;;
          *) rm -rf "${c}" 2>/dev/null || true ;;
        esac
        break
      fi
    done
  fi

  if [ ! -f "${target}/index.html" ]; then
    log_err "Build SPA não encontrado em ${target}/index.html. O domínio vai retornar 404."
    log_err "Procurei em: ${candidates[*]}"
    log_err "Conteúdo de ${app_dir}:"; ls -la "${app_dir}" 2>/dev/null || true
    return 1
  fi

  # Corrige units antigas que ficaram apontando para client/dist. A versão nova
  # do servidor também auto-detecta, mas ajustar o service evita regressão na VPS.
  if [ -f "${unit}" ]; then
    sed -i -E "s|-static +[^ ]*/client/dist|-static ${app_dir}/dist|g" "${unit}" || true
    sed -i -E "s|-static +client/dist|-static ${app_dir}/dist|g" "${unit}" || true
    sed -i -E "s|-static +dist|-static ${app_dir}/dist|g" "${unit}" || true
    systemctl daemon-reload >/dev/null 2>&1 || true
  fi
}

projeto_tem_backend_go() {
  local app_dir="${1:-$APP_DIR}"
  [ -f "${app_dir}/go.mod" ] && [ -d "${app_dir}/cmd/server" ]
}

frontend_only() {
  local app_dir="${1:-$APP_DIR}"
  ! projeto_tem_backend_go "${app_dir}" && [ -f "${app_dir}/package.json" -o -d "${app_dir}/client" ]
}

localizar_raiz_backend_go() {
  local base="$1"
  local mod dir
  [ -d "${base}" ] || return 1
  if [ -f "${base}/go.mod" ] && [ -d "${base}/cmd/server" ]; then
    echo "${base}"
    return 0
  fi
  while IFS= read -r -d '' mod; do
    dir=$(dirname "${mod}")
    if [ -d "${dir}/cmd/server" ]; then
      echo "${dir}"
      return 0
    fi
  done < <(find "${base}" -maxdepth 6 -type f -name go.mod -print0 2>/dev/null)
  return 1
}

localizar_raiz_frontend() {
  local base="$1"
  local pkg dir
  [ -d "${base}" ] || return 1
  if [ -f "${base}/package.json" ] && { [ -d "${base}/src" ] || [ -f "${base}/index.html" ]; }; then
    echo "${base}"
    return 0
  fi
  while IFS= read -r -d '' pkg; do
    dir=$(dirname "${pkg}")
    if [ -d "${dir}/src" ] || [ -f "${dir}/index.html" ]; then
      echo "${dir}"
      return 0
    fi
  done < <(find "${base}" -maxdepth 6 -type f -name package.json -print0 2>/dev/null)
  return 1
}

zip_tem_backend_go() {
  local zip_file="$1"
  local tmp_dir raiz
  [ -f "${zip_file}" ] || return 1
  if ! command -v unzip >/dev/null 2>&1; then
    printf "${YELLOW} >> Pacote unzip não encontrado; instalando para validar ${zip_file}...${WHITE}\n" >&2
    apt-get update -y >/dev/null 2>&1 || return 1
    DEBIAN_FRONTEND=noninteractive apt-get install -y unzip >/dev/null 2>&1 || return 1
  fi
  tmp_dir=$(mktemp -d)
  unzip -q "${zip_file}" -d "${tmp_dir}" >/dev/null 2>&1 || { rm -rf "${tmp_dir}"; return 1; }
  raiz=$(localizar_raiz_backend_go "${tmp_dir}" || true)
  rm -rf "${tmp_dir}"
  [ -n "${raiz}" ]
}

listar_zips_candidatos() {
  # Procura QUALQUER .zip diretamente em /root primeiro (prioridade máxima),
  # depois amplia a busca com nomes conhecidos em outras pastas.
  {
    find /root -maxdepth 1 -type f -iname '*.zip' -printf '%T@ %p\n' 2>/dev/null
    find /root "${APP_HOME}" "${APP_DIR}" /tmp -maxdepth 3 -type f \
      \( -iname '*wacall*.zip' -o -iname '*vozzap*.zip' -o -iname '*sa*.zip' \) \
      -printf '%T@ %p\n' 2>/dev/null
  } | sort -rn | awk '!seen[$2]++ {sub(/^[^ ]+ /, ""); print}'
}

selecionar_zip_backend_go() {
  local zip_file encontrou_zip="nao"
  while IFS= read -r zip_file; do
    [ -f "${zip_file}" ] || continue
    encontrou_zip="sim"
    if zip_tem_backend_go "${zip_file}"; then
      echo "${zip_file}"
      return 0
    fi
    printf "${YELLOW} >> Ignorando zip sem backend Go completo: ${zip_file}${WHITE}\n" >&2
  done < <(listar_zips_candidatos)
  if [ "${encontrou_zip}" != "sim" ]; then
    printf "${YELLOW} >> Nenhum arquivo .zip foi localizado diretamente em /root. Arquivos atuais em /root:${WHITE}\n" >&2
    ls -lah /root 2>/dev/null | sed 's#^#   #' >&2 || true
  fi
  return 1
}

aplicar_pacote_zip() {
  local zip_file="$1"
  local app_dir="${2:-$APP_DIR}"
  local exigir_backend="${3:-nao}"
  local tmp_extract backend_root frontend_root

  [ -f "${zip_file}" ] || { log_err "Arquivo zip não encontrado: ${zip_file}"; return 1; }
  mkdir -p "${app_dir}" || return 1
  command -v unzip >/dev/null 2>&1 || { apt-get update -y >/dev/null 2>&1; apt-get install -y unzip || return 1; }
  tmp_extract=$(mktemp -d)
  unzip -q -o "${zip_file}" -d "${tmp_extract}" || { rm -rf "${tmp_extract}"; log_err "Falha ao extrair ${zip_file}."; return 1; }

  backend_root=$(localizar_raiz_backend_go "${tmp_extract}" || true)
  if [ -n "${backend_root}" ]; then
    log_info "Pacote completo detectado em ${backend_root}."
    # Se o zip NÃO traz client/ nem dist/, preservamos os que já existem no
    # servidor — evita o cenário em que um update só-backend apaga o frontend
    # buildado e o próximo `npm run build` falha por falta de package.json.
    local preserve_client_flag=""
    local preserve_dist_flag=""
    [ ! -d "${backend_root}/client" ] && preserve_client_flag="! -name client"
    [ ! -d "${backend_root}/dist" ]   && preserve_dist_flag="! -name dist"
    log_info "Limpando ${app_dir} antes de aplicar o pacote completo (preservando data/, media/, .env, banco${preserve_client_flag:+, client/}${preserve_dist_flag:+, dist/})..."
    find "${app_dir}" -mindepth 1 -maxdepth 1 \
      ! -name 'data' ! -name 'media' ! -name '.env' \
      ! -name 'wacalls.db' ! -name 'wacalls.db-shm' ! -name 'wacalls.db-wal' \
      ${preserve_client_flag} ${preserve_dist_flag} \
      -exec rm -rf {} + 2>/dev/null || true
    cp -a "${backend_root}/." "${app_dir}/" || { rm -rf "${tmp_extract}"; log_err "Falha ao copiar pacote completo para ${app_dir}."; return 1; }
    rm -rf "${tmp_extract}"
    projeto_tem_backend_go "${app_dir}" || { log_err "Backend Go não ficou válido em ${app_dir}."; return 1; }
    return 0
  fi

  if [ "${exigir_backend}" = "sim" ]; then
    log_err "Após extrair o zip não foi encontrado backend Go completo."
    log_err "O zip correto precisa conter go.mod e cmd/server/ em alguma pasta do pacote. Não use zip somente do frontend."
    log_info "Conteúdo extraído para diagnóstico:"
    find "${tmp_extract}" -maxdepth 3 -type f \( -name 'go.mod' -o -name 'package.json' -o -name 'Dockerfile' \) -print 2>/dev/null | sed 's#^# - #' || true
    rm -rf "${tmp_extract}"
    return 1
  fi

  frontend_root=$(localizar_raiz_frontend "${tmp_extract}" || true)
  if [ -n "${frontend_root}" ]; then
    log_warn "Zip contém somente frontend; aplicando em ${app_dir}/client e preservando backend existente."
    mkdir -p "${app_dir}/client" || { rm -rf "${tmp_extract}"; return 1; }
    find "${app_dir}/client" -mindepth 1 -maxdepth 1 ! -name '.env' -exec rm -rf {} + 2>/dev/null || true
    cp -a "${frontend_root}/." "${app_dir}/client/" || { rm -rf "${tmp_extract}"; log_err "Falha ao copiar frontend para ${app_dir}/client."; return 1; }
    rm -rf "${tmp_extract}"
    return 0
  fi

  rm -rf "${tmp_extract}"
  log_err "Zip sem backend Go e sem frontend React/Vite reconhecido: ${zip_file}"
  return 1
}

instalar_build_frontend() {
  local app_dir="${1:-$APP_DIR}"
  local frontend_dir="${2:-}"
  [ -n "${frontend_dir}" ] || frontend_dir=$(resolver_frontend_dir "${app_dir}") || frontend_dir=""

  # Sem código-fonte do frontend: se já existe um dist/ buildado (do update
  # anterior ou de backup), reaproveita-o em vez de falhar. Só aborta se
  # também não houver dist/ pronto.
  # Cenários que devemos tratar como "sem fonte de frontend":
  #  a) frontend_dir vazio ou sem package.json;
  #  b) resolveu para o app_dir (monorepo) mas client/package.json não existe
  #     — o `npm run build` chama `npm --prefix client ...` e quebra com
  #     ENOENT em client/package.json (foi o erro reportado);
  #  c) frontend_dir aponta para pasta sem index.html/src/.
  local sem_fonte="0"
  if [ -z "${frontend_dir}" ] || [ ! -f "${frontend_dir}/package.json" ]; then
    sem_fonte="1"
  elif [ "${frontend_dir}" = "${app_dir}" ] && [ ! -f "${app_dir}/client/package.json" ]; then
    sem_fonte="1"
  elif ! grep -q '"build"' "${frontend_dir}/package.json" 2>/dev/null; then
    sem_fonte="1"
  fi
  if [ "${sem_fonte}" = "1" ]; then
    if [ -f "${app_dir}/dist/index.html" ]; then
      log_warn "Frontend-source ausente (client/package.json não encontrado) — reaproveitando ${app_dir}/dist já buildado."
      garantir_static_dist "${app_dir}" "${frontend_dir}" 2>/dev/null || true
      return 0
    fi
    log_warn "Frontend-source ausente e sem dist/ pronto — seguindo com o backend; a UI só voltará depois que você restaurar client/ ou dist/."
    return 0
  fi

  # Usa npm install direto em vez de npm ci. Assim o instalador não para nem
  # imprime erro EUSAGE quando package.json e package-lock.json estiverem fora
  # de sincronia; o lockfile é corrigido automaticamente na VPS.
  local build_ok="0"
  if [ "${APP_USER}" = "root" ]; then
    bash -lc "cd '${frontend_dir}' && npm install --no-audit --no-fund && npm run build" && build_ok="1"
  else
    chown -R "${APP_USER}:${APP_USER}" "${app_dir}" 2>/dev/null || true
    sudo -u "${APP_USER}" -H bash -lc "cd '${frontend_dir}' && npm install --no-audit --no-fund && npm run build" && build_ok="1"
  fi
  if [ "${build_ok}" != "1" ]; then
    log_warn "Build npm do frontend falhou; tentando usar dist/ já incluído no pacote ou backup."
    if [ -f "${app_dir}/dist/index.html" ] || [ -f "${frontend_dir}/dist/index.html" ]; then
      garantir_static_dist "${app_dir}" "${frontend_dir}" || return 1
      log_warn "Frontend publicado usando dist/ existente; atualização continuará sem erro npm."
      return 0
    fi
    return 1
  fi
  garantir_static_dist "${app_dir}" "${frontend_dir}" || return 1
}

# Verdadeiro se já existe um backend Wacalls instalado/rodando na VPS,
# mesmo que o zip atual não traga o código Go.
backend_ja_instalado() {
  local app_dir="${1:-$APP_DIR}"
  if systemctl list-unit-files 2>/dev/null | grep -q "^${APP_NAME}\.service"; then
    return 0
  fi
  if [ -x "${app_dir}/wacalls-server" ]; then
    return 0
  fi
  if [ -f "${app_dir}/go.mod" ] || [ -d "${app_dir}/cmd/server" ]; then
    return 0
  fi
  return 1
}

# Descobre a porta atual do backend (env var, systemd unit, nginx antigo, fallback).
descobrir_porta_app() {
  local p="${porta_app:-}"
  if [ -z "${p}" ] && [ -f "/etc/systemd/system/${APP_NAME}.service" ]; then
    p=$(grep -Po 'addr :\K[0-9]+' "/etc/systemd/system/${APP_NAME}.service" 2>/dev/null | head -n1)
  fi
  if [ -z "${p}" ] && [ -f "/etc/nginx/sites-available/${APP_NAME}" ]; then
    p=$(grep -Po 'proxy_pass http://127\.0\.0\.1:\K[0-9]+' "/etc/nginx/sites-available/${APP_NAME}" 2>/dev/null | head -n1)
  fi
  [ -z "${p}" ] && p="3001"
  echo "${p}"
}

# Nginx servindo SPA estático + proxy_pass /api (e websocket) para o backend.
configurar_nginx_hibrido() {
  local app_dir="${1:-$APP_DIR}"
  local src_dir="${app_dir}/dist"
  [ -f "${src_dir}/index.html" ] || { log_err "Build SPA não encontrado em ${src_dir}/index.html."; return 1; }

  local root_dir="/var/www/${APP_NAME}"
  log_info "Publicando frontend em ${root_dir} (modo híbrido: SPA + proxy /api)..."
  mkdir -p "${root_dir}" || return 1
  if command -v rsync >/dev/null 2>&1; then
    rsync -a --delete "${src_dir}/" "${root_dir}/" || return 1
  else
    rm -rf "${root_dir:?}/"* "${root_dir:?}/".[!.]* 2>/dev/null || true
    cp -a "${src_dir}/." "${root_dir}/" || return 1
  fi
  chown -R www-data:www-data "${root_dir}" 2>/dev/null || true
  find "${root_dir}" -type d -exec chmod 755 {} \; 2>/dev/null || true
  find "${root_dir}" -type f -exec chmod 644 {} \; 2>/dev/null || true

  apt-get install -y nginx >/dev/null 2>&1 || return 1

  local server_name="_"
  [ -n "${subdominio:-}" ] && server_name="${subdominio}"

  local p
  p=$(descobrir_porta_app)
  log_info "Backend detectado/usando porta ${p}."

  cat > "/etc/nginx/sites-available/${APP_NAME}" <<EOF
server {
    listen 80;
    server_name ${server_name};

    root ${root_dir};
    index index.html;
    client_max_body_size 50M;

    location /assets/ {
        try_files \$uri =404;
        expires 30d;
        add_header Cache-Control "public, immutable";
    }

    location /api/ {
        proxy_pass http://127.0.0.1:${p};
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }

    location /ws {
        proxy_pass http://127.0.0.1:${p};
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 3600s;
    }

    location /socket.io/ {
        proxy_pass http://127.0.0.1:${p};
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 3600s;
    }

    location / {
        try_files \$uri \$uri/ /index.html;
    }
}
EOF

  ln -sf "/etc/nginx/sites-available/${APP_NAME}" "/etc/nginx/sites-enabled/${APP_NAME}"
  rm -f /etc/nginx/sites-enabled/default
  nginx -t || return 1
  systemctl enable nginx >/dev/null 2>&1 || true
  systemctl reload nginx || systemctl restart nginx || return 1

  if [ "${habilitar_ssl:-nao}" = "sim" ] && [ -n "${subdominio:-}" ]; then
    apt-get install -y certbot python3-certbot-nginx >/dev/null 2>&1 || true
    certbot --nginx -d "${subdominio}" \
      --non-interactive --agree-tos -m "${email_ssl}" --redirect || \
      log_warn "Falha ao emitir SSL automaticamente. Rode manualmente: certbot --nginx -d ${subdominio}"
  fi

  if systemctl is-active --quiet "${APP_NAME}.service" 2>/dev/null; then
    log_ok "Nginx publicado (SPA + /api -> 127.0.0.1:${p}). Backend ${APP_NAME}.service ativo."
  else
    log_warn "Nginx publicado (SPA + /api -> 127.0.0.1:${p}), mas ${APP_NAME}.service NÃO está ativo."
    log_warn "Inicie com: systemctl start ${APP_NAME}.service  |  Logs: journalctl -u ${APP_NAME} -e"
  fi
}

# Escolhe automaticamente entre nginx híbrido (com backend) e somente estático.
configurar_nginx_auto() {
  local app_dir="${1:-$APP_DIR}"
  if backend_ja_instalado "${app_dir}" || projeto_tem_backend_go "${app_dir}"; then
    configurar_nginx_hibrido "${app_dir}"
  else
    log_warn "Nenhum backend detectado — publicando somente o SPA. Rotas /api/* retornarão 404/405 até subir um backend."
    configurar_nginx_frontend_estatico "${app_dir}"
  fi
}

configurar_nginx_frontend_estatico() {
  local app_dir="${1:-$APP_DIR}"
  local src_dir="${app_dir}/dist"
  [ -f "${src_dir}/index.html" ] || { log_err "Build SPA não encontrado em ${src_dir}/index.html."; return 1; }

  # Publica o dist em /var/www/${APP_NAME} para evitar problemas de permissão
  # (ex.: /root tem modo 700 e nginx/www-data não consegue acessar -> 500).
  local root_dir="/var/www/${APP_NAME}"
  log_info "Publicando frontend em ${root_dir}..."
  mkdir -p "${root_dir}" || return 1
  if command -v rsync >/dev/null 2>&1; then
    rsync -a --delete "${src_dir}/" "${root_dir}/" || return 1
  else
    rm -rf "${root_dir:?}/"* "${root_dir:?}/".[!.]* 2>/dev/null || true
    cp -a "${src_dir}/." "${root_dir}/" || return 1
  fi
  chown -R www-data:www-data "${root_dir}" 2>/dev/null || true
  find "${root_dir}" -type d -exec chmod 755 {} \; 2>/dev/null || true
  find "${root_dir}" -type f -exec chmod 644 {} \; 2>/dev/null || true

  log_info "Configurando Nginx para servir o frontend estático..."
  apt-get install -y nginx >/dev/null 2>&1 || return 1

  local server_name="_"
  [ -n "${subdominio:-}" ] && server_name="${subdominio}"

  cat > "/etc/nginx/sites-available/${APP_NAME}" <<EOF
server {
    listen 80;
    server_name ${server_name};

    root ${root_dir};
    index index.html;
    client_max_body_size 50M;

    location / {
        try_files \$uri \$uri/ /index.html;
    }

    location /assets/ {
        try_files \$uri =404;
        expires 30d;
        add_header Cache-Control "public, immutable";
    }
}
EOF

  ln -sf "/etc/nginx/sites-available/${APP_NAME}" "/etc/nginx/sites-enabled/${APP_NAME}"
  rm -f /etc/nginx/sites-enabled/default
  nginx -t || return 1
  systemctl enable nginx >/dev/null 2>&1 || true
  systemctl reload nginx || systemctl restart nginx || return 1

  if [ "${habilitar_ssl:-nao}" = "sim" ] && [ -n "${subdominio:-}" ]; then
    apt-get install -y certbot python3-certbot-nginx >/dev/null 2>&1 || true
    certbot --nginx -d "${subdominio}" \
      --non-interactive --agree-tos -m "${email_ssl}" --redirect || \
      log_warn "Falha ao emitir SSL automaticamente. Rode manualmente: certbot --nginx -d ${subdominio}"
  fi

  log_ok "Frontend estático publicado em ${root_dir} e servido pelo Nginx."
}

detectar_dir_frontend() {
  local app_dir="${1:-$APP_DIR}"
  # 1) layout com client/ (precisa do diretório E do package.json)
  if [ -d "${app_dir}/client" ] && [ -f "${app_dir}/client/package.json" ]; then
    echo "${app_dir}/client"
    return 0
  fi
  # 2) frontend na raiz do projeto
  if [ -f "${app_dir}/package.json" ]; then
    echo "${app_dir}"
    return 0
  fi
  # 3) busca por package.json em até 4 níveis (ignora node_modules/dist)
  local found
  found=$(find "${app_dir}" -maxdepth 4 -name package.json \
            -not -path '*/node_modules/*' \
            -not -path '*/dist/*' \
            2>/dev/null | head -n1)
  if [ -n "${found}" ] && [ -f "${found}" ]; then
    dirname "${found}"
    return 0
  fi
  # 4) fallback — devolve app_dir mesmo (validação acontece no chamador)
  echo "${app_dir}"
  return 1
}

# Detecta frontend. Não aborta quando package.json está ausente: updates
# backend-only ou pacotes com dist/ pronto devem continuar sem quebrar em
# ENOENT /root/wacalls/client/package.json.
resolver_frontend_dir() {
  local app_dir="${1:-$APP_DIR}"
  local d
  d=$(detectar_dir_frontend "${app_dir}")
  if [ -z "${d}" ] || [ ! -d "${d}" ] || [ ! -f "${d}/package.json" ]; then
    log_warn "Frontend source não localizado (package.json ausente). Vou tentar reaproveitar dist/ existente ou seguir somente com backend."
    log_warn "Conteúdo de ${app_dir}:"
    ls -la "${app_dir}" 2>/dev/null || true
    echo "${d:-$app_dir}"
    return 0
  fi
  echo "${d}"
  return 0
}

# -------------------- Pré-checagens --------------------

if [ "$EUID" -ne 0 ]; then
  printf "${RED} >> Este script precisa ser executado como root (sudo).${WHITE}\n"
  exit 1
fi

if ! grep -q "Ubuntu" /etc/os-release 2>/dev/null; then
  printf "${YELLOW} >> Aviso: distribuição não-Ubuntu detectada. Suportado oficialmente: Ubuntu 22.04.${WHITE}\n"
fi

# -------------------- UI --------------------
banner() {
  clear
  printf "${BLUE}\n"
  printf "  ██╗    ██╗ █████╗  ██████╗ █████╗ ██╗     ██╗     ███████╗\n"
  printf "  ██║    ██║██╔══██╗██╔════╝██╔══██╗██║     ██║     ██╔════╝\n"
  printf "  ██║ █╗ ██║███████║██║     ███████║██║     ██║     ███████╗\n"
  printf "  ██║███╗██║██╔══██║██║     ██╔══██║██║     ██║     ╚════██║\n"
  printf "  ╚███╔███╔╝██║  ██║╚██████╗██║  ██║███████╗███████╗███████║\n"
  printf "   ╚══╝╚══╝ ╚═╝  ╚═╝ ╚═════╝╚═╝  ╚═╝╚══════╝╚══════╝╚══════╝\n"
  printf "${WHITE}\n"
  printf "  ${CYAN}WaCalls${WHITE} — ${GREEN}gratuito${WHITE} • Instalador para ${GREEN}Ubuntu 22.04${WHITE}\n"
  printf "  ${MAGENTA}100%% Open Source${WHITE} — código aberto sob licença MIT\n"
  printf "  ${WHITE}Versão do instalador: ${BLUE}1.0${WHITE}\n"
  printf "  ${YELLOW}IP da VPS: ${CYAN}${ip_atual}${WHITE}   ${YELLOW}Data: ${CYAN}$(date '+%d/%m/%Y %H:%M:%S')${WHITE}\n"
  printf "\n"
  printf "  ${CYAN}Repositório:${WHITE}    ${BLUE}https://github.com/raphaelbat/wacalls-chat${WHITE}\n"
  printf "  ${CYAN}Originado de:${WHITE}   ${BLUE}https://github.com/JotaDev66/WaCalls${WHITE}\n"
  printf "  ${CYAN}Créditos originais:${WHITE} ${GREEN}JotaDev66${WHITE} • ${GREEN}jobasfernandes${WHITE} • ${GREEN}Canal Vem Fazer${WHITE} (${BLUE}https://www.youtube.com/@vemfazer${WHITE})\n\n"

}

log_ok()   { printf "${GREEN} >> %s${WHITE}\n" "$*"; }
log_info() { printf "${CYAN} >> %s${WHITE}\n" "$*"; }
log_warn() { printf "${YELLOW} >> %s${WHITE}\n" "$*"; }
log_err()  { printf "${RED} >> %s${WHITE}\n" "$*"; }

trata_erro() {
  log_err "Erro encontrado na etapa $1. Encerrando."
  echo "$1" > "$ARQUIVO_ETAPAS"
  exit 1
}

# -------------------- Checkpoint --------------------
salvar_etapa()    { echo "$1" > "$ARQUIVO_ETAPAS"; }
carregar_etapa()  { [ -f "$ARQUIVO_ETAPAS" ] && cat "$ARQUIVO_ETAPAS" || echo "0"; }

salvar_variaveis() {
  cat > "$ARQUIVO_VARIAVEIS" <<EOF
subdominio=${subdominio}
email_ssl=${email_ssl}
porta_app=${porta_app}
repo_git=${repo_git}
branch_git=${branch_git}
habilitar_ssl=${habilitar_ssl}
app_user=${APP_USER}
app_home=${APP_HOME}
app_dir=${APP_DIR}
EOF
}

carregar_variaveis() {
  if [ -f "$ARQUIVO_VARIAVEIS" ]; then
    # shellcheck disable=SC1090
    source "$ARQUIVO_VARIAVEIS"
    if [ -n "${app_user:-}" ]; then
      APP_USER="${app_user}"
      APP_HOME="${app_home:-/home/${app_user}}"
      [ "${APP_USER}" = "root" ] && APP_HOME="/root"
      APP_DIR="${app_dir:-${APP_HOME}/${APP_NAME}}"
    fi
  fi
}

# -------------------- Entrada de dados --------------------
coletar_dados() {
  banner
  printf "${YELLOW}═══════════════ Configuração da instalação ═══════════════${WHITE}\n\n"

  APP_USER="root"
  APP_HOME="/root"
  APP_DIR="${APP_HOME}/${APP_NAME}"
  log_info "Instalação em: ${APP_DIR}  (usuário: ${APP_USER})"

  read -p "Subdomínio público (ex: calls.seudominio.com) [vazio = sem nginx/SSL]: " subdominio
  if [ -n "$subdominio" ]; then
    read -p "E-mail para emissão do certificado SSL (Let's Encrypt): " email_ssl
    habilitar_ssl="sim"
  else
    habilitar_ssl="nao"
  fi

  local porta_sugerida
  porta_sugerida=$(detectar_porta_livre 8080)
  log_info "Porta livre detectada automaticamente: ${porta_sugerida}"
  read -p "Porta interna do servidor Go [${porta_sugerida}] (Enter = automática): " porta_app
  porta_app=${porta_app:-$porta_sugerida}

  # Se já existir o projeto completo em ${APP_DIR}, usa direto. Se existir só
  # frontend de tentativas antigas, NÃO segue em modo estático numa instalação nova:
  # procura um zip completo para substituir e instalar backend + frontend.
  # PRIORIDADE: se existir um .zip em /root com backend Go completo, ele é a
  # fonte de verdade — extrai por cima do APP_DIR (preservando data/.env/db).
  # Só cai no código já presente em APP_DIR se NÃO houver zip disponível.
  zip_auto=$(selecionar_zip_backend_go || true)
  if [ -n "${zip_auto}" ] && [ -f "${zip_auto}" ]; then
    log_ok "Zip completo detectado em: ${zip_auto}"
    log_info "Extraindo ${zip_auto} em ${APP_DIR} (substituindo código antigo, preservando data/.env/banco) ..."
    aplicar_pacote_zip "${zip_auto}" "${APP_DIR}" "sim" || exit 1
    [ "${APP_USER}" != "root" ] && chown -R "${APP_USER}:${APP_USER}" "${APP_DIR}" || true
    log_ok "Código atualizado a partir do zip em ${APP_DIR}."
    repo_git="zip:${zip_auto}"
    branch_git="zip"
  elif projeto_tem_backend_go "${APP_DIR}"; then
    log_ok "Nenhum zip em /root — usando código já presente em ${APP_DIR} (sem git clone)."
    repo_git="local"
    branch_git="local"
  else
    log_err "Nenhum zip completo com backend Go foi encontrado e nenhum código completo existe em ${APP_DIR}."
    log_err "Coloque o arquivo .zip em /root (ex: /root/vozzap-chat.zip ou /root/wacalls.zip) e rode novamente."
    log_info "Diagnóstico rápido:"
    log_info "Zips detectados:"
    find /root -maxdepth 1 -type f -iname '*.zip' -printf ' - %p (%s bytes)\n' 2>/dev/null || true
    log_info "Código esperado dentro do zip: go.mod e cmd/server/."
    exit 1
  fi

  salvar_variaveis
  echo
  log_info "Configuração salva em ${ARQUIVO_VARIAVEIS}."
  sleep 1
}

# -------------------- Etapas --------------------
etapa1_sistema() {
  banner
  log_info "[1/8] Atualizando sistema e instalando pacotes base..."
  apt-get update -y || trata_erro 1
  DEBIAN_FRONTEND=noninteractive apt-get upgrade -y || trata_erro 1
  DEBIAN_FRONTEND=noninteractive apt-get install -y \
    curl wget git build-essential pkg-config ca-certificates \
    ufw lsb-release software-properties-common unzip ffmpeg \
    autoconf automake libtool m4 cmake openssl xxd \
    || trata_erro 1
  salvar_etapa 1
  log_ok "Pacotes base instalados."
}

etapa2_usuario() {
  banner
  log_info "[2/8] Preparando usuário/diretório da aplicação..."
  if [ "${APP_USER}" != "root" ]; then
    if ! id "${APP_USER}" &>/dev/null; then
      log_info "Criando usuário do sistema: ${APP_USER}"
      useradd -m -s /bin/bash "${APP_USER}" || trata_erro 2
      # libera acesso sem senha pra sudo opcional (não obrigatório)
      usermod -aG sudo "${APP_USER}" || true
    fi
    APP_HOME=$(getent passwd "${APP_USER}" | cut -d: -f6)
    APP_DIR="${APP_HOME}/${APP_NAME}"
    salvar_variaveis
  fi
  mkdir -p "${APP_DIR}" || trata_erro 2
  chown -R "${APP_USER}:${APP_USER}" "${APP_HOME}" 2>/dev/null || true
  chown -R "${APP_USER}:${APP_USER}" "${APP_DIR}" || trata_erro 2
  log_ok "Diretório ${APP_DIR} pronto (dono: ${APP_USER})."
  salvar_etapa 2
}

etapa3_go() {
  banner
  if frontend_only "${APP_DIR}"; then
    log_info "[3/7] Projeto sem backend Go detectado — pulando instalação do Go."
    salvar_etapa 3
    return 0
  fi
  # Lê a versão exigida pelo go.mod (se o repo já foi clonado) ou usa fallback
  local go_version="${GO_VERSION_FALLBACK}"
  local mod_file="${APP_DIR}/go.mod"
  if [ -f "${mod_file}" ]; then
    local v
    v=$(awk '/^go [0-9]+\.[0-9]+/ {print $2; exit}' "${mod_file}")
    [ -n "$v" ] && go_version="$v"
  fi
  # Normaliza pra X.Y.Z (go.dev exige patch)
  case "$go_version" in
    *.*.*) ;;
    *.*)   go_version="${go_version}.0" ;;
  esac
  log_info "[3/7] Instalando Go ${go_version}..."
  if command -v go &>/dev/null && go version | grep -q "go${go_version}"; then
    log_info "Go ${go_version} já instalado."
  else
    cd /tmp || trata_erro 3
    local arch="amd64"
    [ "$(uname -m)" = "aarch64" ] && arch="arm64"
    if ! wget -q "https://go.dev/dl/go${go_version}.linux-${arch}.tar.gz" -O go.tar.gz; then
      log_warn "Versão ${go_version} indisponível, tentando fallback ${GO_VERSION_FALLBACK}."
      go_version="${GO_VERSION_FALLBACK}"
      wget -q "https://go.dev/dl/go${go_version}.linux-${arch}.tar.gz" -O go.tar.gz || trata_erro 3
    fi
    rm -rf /usr/local/go
    tar -C /usr/local -xzf go.tar.gz || trata_erro 3
    rm -f go.tar.gz
    cat > /etc/profile.d/go.sh <<'EOF'
export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin
EOF
    chmod +x /etc/profile.d/go.sh
  fi
  export PATH=$PATH:/usr/local/go/bin
  go version || trata_erro 3
  salvar_etapa 3
  log_ok "Go pronto."
}

etapa4_node() {
  banner
  log_info "[4/7] Instalando Node.js ${NODE_MAJOR}.x..."
  if ! command -v node &>/dev/null || ! node -v | grep -q "^v${NODE_MAJOR}\."; then
    curl -fsSL "https://deb.nodesource.com/setup_${NODE_MAJOR}.x" | bash - || trata_erro 4
    apt-get install -y nodejs || trata_erro 4
  fi
  node -v && npm -v
  salvar_etapa 4
  log_ok "Node.js pronto."
}

# -------------------- Redis + SQLite (compatível com whatsmeow) --------------------
# O WaCalls usa SQLite como banco principal porque a versão atual do whatsmeow
# aceita os dialetos SQLite/Postgres no sqlstore, mas NÃO aceita mysql/mariadb.
# MariaDB causava panic: unknown dialect 'mysql'. Redis continua opcional para
# cache/fanout e é seguro manter no .env via REDIS_URL.
etapa_mariadb_redis() {
  banner
  if frontend_only "${APP_DIR}"; then
    log_info "[DB] Projeto frontend-only detectado — pulando Redis."
    return 0
  fi
  log_info "[DB] Configurando SQLite + Redis (sem MariaDB para evitar panic do whatsmeow)..."

  DEBIAN_FRONTEND=noninteractive apt-get install -y \
    redis-server \
    || trata_erro 5

  systemctl enable --now redis-server >/dev/null 2>&1 || true

  local env_file="${APP_DIR}/.env"
  mkdir -p "${APP_DIR}"

  local redis_pass
  if [ -f "${env_file}" ] && grep -q '^REDIS_URL=' "${env_file}"; then
    redis_pass=$(grep '^REDIS_URL=' "${env_file}" 2>/dev/null | sed -E 's|.*://:([^@]+)@.*|\1|')
    [ -z "${redis_pass}" ] && redis_pass=$(openssl rand -hex 16)
  else
    redis_pass=$(openssl rand -hex 16 2>/dev/null || head -c 16 /dev/urandom | xxd -p)
  fi

  # Redis: define senha (requirepass) se ainda não estiver setada
  if [ -f /etc/redis/redis.conf ]; then
    if grep -qE '^\s*requirepass\s+' /etc/redis/redis.conf; then
      sed -i -E "s|^\s*requirepass\s+.*|requirepass ${redis_pass}|" /etc/redis/redis.conf
    else
      echo "requirepass ${redis_pass}" >> /etc/redis/redis.conf
    fi
    systemctl restart redis-server >/dev/null 2>&1 || true
  fi

  cat > "${env_file}" <<EOF
# Gerado pelo instalador WaCalls
# Banco principal: SQLite em ${APP_DIR}/wacalls.db
# Não definir DB_DRIVER=mariadb: o whatsmeow não aceita dialeto mysql/mariadb.
REDIS_URL=redis://:${redis_pass}@127.0.0.1:6379/0
EOF
  chmod 600 "${env_file}"
  [ "${APP_USER}" != "root" ] && chown "${APP_USER}:${APP_USER}" "${env_file}" || true

  log_ok "SQLite + Redis prontos. .env corrigido em ${env_file}."

  # Se o serviço wacalls já existir (reconfiguração), reinicia pra carregar o .env novo
  if systemctl is-active --quiet "${APP_NAME}.service" 2>/dev/null; then
    log_info "Reiniciando ${APP_NAME} para aplicar nova configuração de banco..."
    systemctl restart "${APP_NAME}.service" || true
  fi
}

etapa6_clone_build() {
  banner
  log_info "[5/7] Preparando código e compilando o WaCalls..."
  local app_dir="${APP_DIR}"

  # Se estamos usando um zip do /root, sempre re-extrai para garantir integridade
  if [[ "${repo_git}" == zip:* ]]; then
    local zip_src="${repo_git#zip:}"
    if [ -f "${zip_src}" ]; then
      log_info "Re-extraindo ${zip_src} em ${app_dir} para garantir código completo..."
      aplicar_pacote_zip "${zip_src}" "${app_dir}" "sim" || trata_erro 6
    fi
  elif [ -f "${app_dir}/go.mod" ] || [ -f "${app_dir}/package.json" ]; then
    log_ok "Usando código já presente em ${app_dir} (sem git)."
  elif [ -d "${app_dir}/.git" ]; then
    log_info "Repositório git encontrado — atualizando..."
    git -C "${app_dir}" fetch --all || trata_erro 6
    git -C "${app_dir}" reset --hard "origin/${branch_git}" || trata_erro 6
  else
    log_info "Clonando ${repo_git} (branch ${branch_git})..."
    git clone -b "${branch_git}" "${repo_git}" "${app_dir}" || trata_erro 6
  fi

  if projeto_tem_backend_go "${app_dir}"; then
    if [ ! -d "${app_dir}/internal/voip/media" ]; then
      # Última tentativa: procurar um zip em /root e re-extrair
      local zip_fallback
      zip_fallback=$(selecionar_zip_backend_go || true)
      if [ -n "${zip_fallback}" ] && [ -f "${zip_fallback}" ]; then
        log_warn "internal/voip/media ausente — re-extraindo ${zip_fallback}..."
        aplicar_pacote_zip "${zip_fallback}" "${app_dir}" "sim" || trata_erro 6
      fi
    fi
    if [ ! -d "${app_dir}/internal/voip/media" ]; then
      log_err "Pasta interna internal/voip/media ausente em ${app_dir}. Coloque o wacalls.zip atualizado em /root e rode de novo."
      trata_erro 6
    fi

    log_info "Baixando dependências Go..."
    bash -lc "export PATH=\$PATH:/usr/local/go/bin && cd '${app_dir}' && go mod download" || trata_erro 6
  else
    log_warn "Backend Go não encontrado em ${app_dir}; instalando como frontend estático."
  fi

  log_info "Compilando cliente React..."
  local frontend_dir
  frontend_dir=$(resolver_frontend_dir "${app_dir}") || trata_erro 6
  log_info "Frontend detectado em: ${frontend_dir}"
  instalar_build_frontend "${app_dir}" "${frontend_dir}" || trata_erro 6

  if ! projeto_tem_backend_go "${app_dir}"; then
    configurar_nginx_auto "${app_dir}" || trata_erro 6
    chown -R "${APP_USER}:${APP_USER}" "${app_dir}" 2>/dev/null || true
    salvar_etapa 6
    log_ok "Build frontend-only concluído em ${app_dir}/dist."
    return 0
  fi

  log_info "Compilando servidor Go (puro-Go, sem CGO)..."
  if [ "${APP_USER}" = "root" ]; then
    bash -lc "export PATH=\$PATH:/usr/local/go/bin && export CGO_ENABLED=0 && export GO111MODULE=on && \
       cd '${app_dir}' && go build -o wacalls-server ./cmd/server" || trata_erro 6
  else
    sudo -u "${APP_USER}" -H bash -lc "export PATH=\$PATH:/usr/local/go/bin && export CGO_ENABLED=0 && export GO111MODULE=on && \
       cd '${app_dir}' && go build -o wacalls-server ./cmd/server" || trata_erro 6
  fi
  chown -R "${APP_USER}:${APP_USER}" "${app_dir}" 2>/dev/null || true

  salvar_etapa 6
  log_ok "Build concluído em ${app_dir}/wacalls-server."
}

etapa7_systemd() {
  banner
  log_info "[6/7] Criando serviço systemd..."
  local app_dir="${APP_DIR}"

  if ! projeto_tem_backend_go "${app_dir}"; then
    log_info "Projeto frontend-only — nenhum serviço ${APP_NAME} será criado. O Nginx servirá ${app_dir}/dist."
    configurar_nginx_auto "${app_dir}" || trata_erro 7
    salvar_etapa 7
    return 0
  fi

  cat > "/etc/systemd/system/${APP_NAME}.service" <<EOF
[Unit]
Description=WaCalls (Go) - WhatsApp voice calls server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${APP_USER}
Group=${APP_USER}
WorkingDirectory=${app_dir}
Environment=WACALLS_MEDIA_READY_TIMEOUT_SECONDS=25
# Banco/cache: SQLite em -db /root/wacalls/wacalls.db + Redis via EnvironmentFile
# Não use DB_DRIVER=mariadb: whatsmeow não aceita o dialeto mysql/mariadb.
# Environment=REDIS_URL=redis://:senha@127.0.0.1:6379/0
EnvironmentFile=-${app_dir}/.env
ExecStart=${app_dir}/wacalls-server -addr :${porta_app} -static ${app_dir}/dist -db ${app_dir}/wacalls.db
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable "${APP_NAME}.service"
  systemctl restart "${APP_NAME}.service"
  sleep 2
  systemctl --no-pager --full status "${APP_NAME}.service" || true

  salvar_etapa 7
  log_ok "Serviço ${APP_NAME} ativo na porta ${porta_app}."
}

etapa8_nginx_ssl() {
  banner
  if ! projeto_tem_backend_go "${APP_DIR}"; then
    log_info "[7/7] Projeto frontend-only — reaplicando Nginx estático."
    configurar_nginx_auto "${APP_DIR}" || trata_erro 8
    salvar_etapa 8
    return 0
  fi
  if [ "$habilitar_ssl" != "sim" ] || [ -z "$subdominio" ]; then
    log_info "[7/7] Sem subdomínio definido — pulando nginx/SSL."
    log_info "Acesse direto: http://${ip_atual}:${porta_app}"
    salvar_etapa 8
    return 0
  fi

  log_info "[7/7] Configurando nginx + Let's Encrypt para ${subdominio}..."
  apt-get install -y nginx certbot python3-certbot-nginx || trata_erro 8

  cat > "/etc/nginx/sites-available/${APP_NAME}" <<EOF
server {
    listen 80;
    server_name ${subdominio};

    client_max_body_size 50M;

    location / {
        proxy_pass http://127.0.0.1:${porta_app};
        proxy_http_version 1.1;
        proxy_set_header Upgrade           \$http_upgrade;
        proxy_set_header Connection        "upgrade";
        proxy_set_header Host              \$host;
        proxy_set_header X-Real-IP         \$remote_addr;
        proxy_set_header X-Forwarded-For   \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_read_timeout  3600s;
        proxy_send_timeout  3600s;
        proxy_buffering     off;
        chunked_transfer_encoding off;
    }
}
EOF

  ln -sf "/etc/nginx/sites-available/${APP_NAME}" "/etc/nginx/sites-enabled/${APP_NAME}"
  rm -f /etc/nginx/sites-enabled/default
  nginx -t || trata_erro 8
  systemctl reload nginx

  certbot --nginx -d "${subdominio}" \
    --non-interactive --agree-tos -m "${email_ssl}" --redirect || \
    log_warn "Falha ao emitir SSL automaticamente. Rode manualmente: certbot --nginx -d ${subdominio}"

  salvar_etapa 8
  log_ok "Nginx configurado em https://${subdominio}"
}

etapa_firewall() {
  log_info "Configurando firewall (UFW)..."
  ufw allow OpenSSH >/dev/null 2>&1 || true
  ufw allow 80/tcp  >/dev/null 2>&1 || true
  ufw allow 443/tcp >/dev/null 2>&1 || true
  yes | ufw enable >/dev/null 2>&1 || true
}

# -------------------- Piper TTS (Wyoming) --------------------
etapa_piper() {
  banner
  log_info "[TTS] Instalando Piper (Wyoming) + wrapper HTTP em 127.0.0.1:5005..."

  # 1) Docker
  if ! command -v docker >/dev/null 2>&1; then
    log_info "Instalando Docker..."
    apt-get update -y >/dev/null 2>&1 || true
    apt-get install -y ca-certificates curl gnupg lsb-release >/dev/null 2>&1 || true
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg 2>/dev/null || true
    chmod a+r /etc/apt/keyrings/docker.gpg 2>/dev/null || true
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" \
      > /etc/apt/sources.list.d/docker.list
    apt-get update -y >/dev/null 2>&1 || true
    apt-get install -y docker-ce docker-ce-cli containerd.io || apt-get install -y docker.io || log_warn "Falha instalando docker"
    systemctl enable --now docker >/dev/null 2>&1 || true
  fi

  # 2) Container wyoming-piper (porta 10200, voz PT-BR)
  mkdir -p /var/lib/piper-data
  if docker ps -a --format '{{.Names}}' | grep -q '^piper$'; then
    log_info "Atualizando container piper..."
    docker pull rhasspy/wyoming-piper >/dev/null 2>&1 || true
    docker rm -f piper >/dev/null 2>&1 || true
  fi
  docker run -d --name piper --restart unless-stopped \
    -p 127.0.0.1:10200:10200 \
    -v /var/lib/piper-data:/data \
    rhasspy/wyoming-piper \
    --voice pt_BR-faber-medium >/dev/null 2>&1 || log_warn "Falha ao iniciar container piper"

  # 3) Dependências do wrapper e conversão de áudio (ElevenLabs MP3 -> PCM da chamada)
  apt-get install -y python3 python3-pip python3-venv ffmpeg espeak-ng >/dev/null 2>&1 || true
  python3 -m venv /opt/piper-bridge >/dev/null 2>&1 || true
  /opt/piper-bridge/bin/pip install --quiet --upgrade pip >/dev/null 2>&1 || true
  /opt/piper-bridge/bin/pip install --quiet aiohttp wyoming >/dev/null 2>&1 || \
    log_warn "Falha ao instalar wyoming/aiohttp"

  log_info "Pré-aquecendo voz Piper pt_BR-faber-medium (primeira vez pode demorar)..."
  timeout 180 docker exec piper python3 - <<'PYEOF' >/dev/null 2>&1 || true
from pathlib import Path
try:
    from piper.voice import PiperVoice
    model = None
    for path in Path('/data').rglob('pt_BR-faber-medium.onnx'):
        model = str(path)
        break
    if model:
        voice = PiperVoice.load(model)
        with open('/tmp/piper-warmup.raw', 'wb') as f:
            voice.synthesize('olá', f)
except Exception:
    pass
PYEOF

  # 4) Wrapper HTTP: POST /api/tts {text, voice?} -> PCM 16k LE mono
  cat > /opt/piper-bridge/bridge.py <<'PYEOF'
#!/usr/bin/env python3
import asyncio, audioop, hashlib, os
from pathlib import Path
from aiohttp import web
from wyoming.client import AsyncTcpClient
from wyoming.tts import Synthesize, SynthesizeVoice
from wyoming.audio import AudioChunk, AudioStop

PIPER_HOST = os.environ.get("PIPER_HOST", "127.0.0.1")
PIPER_PORT = int(os.environ.get("PIPER_PORT", "10200"))
BIND_HOST  = os.environ.get("BIND_HOST", "127.0.0.1")
BIND_PORT  = int(os.environ.get("BIND_PORT", "5005"))
PIPER_TIMEOUT = int(os.environ.get("PIPER_TIMEOUT", "210"))
CACHE_DIR = Path(os.environ.get("PIPER_CACHE_DIR", "/var/cache/piper-bridge"))
CACHE_DIR.mkdir(parents=True, exist_ok=True)
LOCK = asyncio.Lock()

def cache_path(text: str, voice: str | None) -> Path:
    h = hashlib.sha1(((voice or "") + "\0" + text).encode("utf-8")).hexdigest()
    return CACHE_DIR / f"{h}.pcm"

async def synth(text: str, voice: str | None) -> bytes:
    cp = cache_path(text, voice)
    if cp.exists() and cp.stat().st_size > 0:
        return cp.read_bytes()
    # Piper/Wyoming can block during first voice load. Serialize generation so
    # multiple inbound calls do not all trigger the expensive warm-up at once.
    async with LOCK:
        if cp.exists() and cp.stat().st_size > 0:
            return cp.read_bytes()
        async with AsyncTcpClient(PIPER_HOST, PIPER_PORT) as client:
            sv = SynthesizeVoice(name=voice) if voice else None
            await client.write_event(Synthesize(text=text, voice=sv).event())
            pcm = bytearray()
            rate = 22050
            width = 2
            channels = 1
            while True:
                ev = await asyncio.wait_for(client.read_event(), timeout=PIPER_TIMEOUT)
                if ev is None:
                    break
                if AudioChunk.is_type(ev.type):
                    ch = AudioChunk.from_event(ev)
                    rate, width, channels = ch.rate, ch.width, ch.channels
                    pcm.extend(ch.audio)
                elif AudioStop.is_type(ev.type):
                    break
        data = bytes(pcm)
        if not data:
            raise RuntimeError("piper returned empty audio")
        if channels == 2:
            data = audioop.tomono(data, width, 1, 1)
        if width != 2:
            data = audioop.lin2lin(data, width, 2)
            width = 2
        if rate != 16000:
            data, _ = audioop.ratecv(data, width, 1, rate, 16000, None)
        tmp = cp.with_suffix(".tmp")
        tmp.write_bytes(data)
        tmp.replace(cp)
        return data

async def handle_tts(request: web.Request):
    try:
        body = await request.json()
    except Exception:
        body = {}
    text = (body.get("text") or "").strip()
    voice = body.get("voice") or None
    if not text:
        return web.Response(status=400, text="missing text")
    try:
        pcm = await asyncio.wait_for(synth(text, voice), timeout=PIPER_TIMEOUT + 10)
    except Exception as e:
        return web.Response(status=502, text=f"piper error: {e}")
    return web.Response(body=pcm, content_type="application/octet-stream")

async def healthz(_):
    return web.Response(text="ok")

app = web.Application()
app.router.add_post("/api/tts", handle_tts)
app.router.add_get("/healthz", healthz)

if __name__ == "__main__":
    web.run_app(app, host=BIND_HOST, port=BIND_PORT, print=None)
PYEOF

  # 5) systemd unit do wrapper
  cat > /etc/systemd/system/piper-bridge.service <<'EOF'
[Unit]
Description=Piper HTTP bridge (Wyoming -> PCM16k)
After=docker.service network-online.target
Wants=docker.service network-online.target

[Service]
Type=simple
Environment=PIPER_TIMEOUT=210
Environment=PIPER_CACHE_DIR=/var/cache/piper-bridge
ExecStart=/opt/piper-bridge/bin/python /opt/piper-bridge/bridge.py
Restart=always
RestartSec=3
TimeoutStartSec=180

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable piper-bridge.service >/dev/null 2>&1 || true
  systemctl restart piper-bridge.service || log_warn "Falha ao iniciar piper-bridge"

  sleep 2
  if curl -sf http://127.0.0.1:5005/healthz >/dev/null 2>&1; then
    log_info "Aquecendo endpoint TTS para evitar timeout na primeira URA..."
    curl --max-time 180 -sf \
      -H 'Content-Type: application/json' \
      -o /tmp/wacalls-piper-warmup.pcm \
      -d '{"text":"olá","voice":"pt_BR-faber-medium"}' \
      http://127.0.0.1:5005/api/tts >/dev/null 2>&1 || \
      log_warn "Warm-up do Piper não respondeu ainda; veja: journalctl -u piper-bridge -n 80"
    rm -f /tmp/wacalls-piper-warmup.pcm >/dev/null 2>&1 || true
    log_ok "Piper TTS pronto em http://127.0.0.1:5005/api/tts (voz pt_BR-faber-medium)."
  else
    log_warn "piper-bridge ainda inicializando. Verifique: systemctl status piper-bridge"
  fi
}

# -------------------- Orquestração --------------------
executar_instalacao() {
  local atual
  atual=$(carregar_etapa)
  log_info "Retomando da etapa: ${atual}"

  [ "$atual" -lt 1 ] && etapa1_sistema
  [ "$atual" -lt 2 ] && etapa2_usuario
  [ "$atual" -lt 3 ] && etapa3_go
  [ "$atual" -lt 4 ] && etapa4_node
  [ "$atual" -lt 6 ] && etapa6_clone_build
  if projeto_tem_backend_go "${APP_DIR}"; then
    etapa_mariadb_redis
    # Piper (TTS) e Whisper (STT) removidos: não usados por este backend.
  else
    log_info "Projeto frontend-only — pulando Redis."
  fi
  [ "$atual" -lt 7 ] && etapa7_systemd
  [ "$atual" -lt 8 ] && etapa8_nginx_ssl
  etapa_firewall
  etapa_seed_admin

  banner
  printf "${GREEN}╔══════════════════════════════════════════════════════════════╗${WHITE}\n"
  printf "${GREEN}║${WHITE}              ✅  Instalação concluída com sucesso             ${GREEN}║${WHITE}\n"
  printf "${GREEN}╚══════════════════════════════════════════════════════════════╝${WHITE}\n\n"
  if [ "$habilitar_ssl" = "sim" ] && [ -n "$subdominio" ]; then
    printf "  ${CYAN}URL pública: ${WHITE}https://${subdominio}\n"
  fi
  printf "  ${CYAN}URL local:   ${WHITE}http://${ip_atual}:${porta_app}\n"
  if projeto_tem_backend_go "${APP_DIR}"; then
    printf "  ${CYAN}Serviço:     ${WHITE}systemctl status ${APP_NAME}\n"
    printf "  ${CYAN}Logs:        ${WHITE}journalctl -u ${APP_NAME} -f\n"
  else
    printf "  ${CYAN}Servidor:    ${WHITE}Nginx estático (${APP_DIR}/dist)\n"
  fi
  printf "  ${CYAN}Diretório:   ${WHITE}${APP_DIR}\n\n"

  local cred_file="/root/wacalls-credenciais.txt"
  if [ -f "${cred_file}" ]; then
    local admin_user admin_pass
    admin_user=$(grep -E '^usuario=' "${cred_file}" | cut -d= -f2-)
    admin_pass=$(grep -E '^senha='   "${cred_file}" | cut -d= -f2-)
    printf "${GREEN}╔══════════════════════════════════════════════════════════════╗${WHITE}\n"
    printf "${GREEN}║${WHITE}                     🔐  Acesso ao painel                      ${GREEN}║${WHITE}\n"
    printf "${GREEN}╚══════════════════════════════════════════════════════════════╝${WHITE}\n"
    printf "  ${CYAN}Usuário: ${WHITE}${admin_user}\n"
    printf "  ${CYAN}Senha:   ${WHITE}${admin_pass}\n"
    printf "  ${YELLOW}(salvo em ${cred_file} — troque a senha após o primeiro login)${WHITE}\n\n"
  fi
  printf "  ${MAGENTA}WaCalls é open source e gratuito — contribua no GitHub!${WHITE}\n\n"
}

# -------------------- Seed do admin (SQLite + bcrypt) --------------------
# Insere um usuário admin diretamente no banco na primeira execução, para que
# a tela de login funcione sem precisar de tela de registro.
etapa_seed_admin() {
  if ! projeto_tem_backend_go "${APP_DIR}"; then
    return 0
  fi

  local cred_file="/root/wacalls-credenciais.txt"
  local db_file="${APP_DIR}/wacalls.db"
  # Credenciais fixas do admin padrão do sistema
  local admin_user="wacalls@admin.com"
  local admin_pass="admin"

  # Garante ferramentas: sqlite3 CLI + htpasswd (bcrypt)
  command -v sqlite3  >/dev/null 2>&1 || apt-get install -y sqlite3 >/dev/null 2>&1
  command -v htpasswd >/dev/null 2>&1 || apt-get install -y apache2-utils >/dev/null 2>&1

  # Aguarda o wacalls.db (criado pelo backend na primeira subida)
  log_info "Aguardando o banco em ${db_file}..."
  local i=0
  while [ ! -f "${db_file}" ] && [ $i -lt 30 ]; do
    sleep 1; i=$((i+1))
  done
  if [ ! -f "${db_file}" ]; then
    systemctl restart "${APP_NAME}" 2>/dev/null || true
    i=0
    while [ ! -f "${db_file}" ] && [ $i -lt 30 ]; do
      sleep 1; i=$((i+1))
    done
  fi
  if [ ! -f "${db_file}" ]; then
    log_warn "Banco ${db_file} não foi criado — pulei o seed do admin."
    return 0
  fi

  # Gera hash bcrypt cost=12 (o backend usa 12)
  local hash
  hash=$(htpasswd -nbBC 12 "" "${admin_pass}" 2>/dev/null | tr -d ':\n' | sed 's|^\$2y\$|\$2a\$|')
  if [ -z "${hash}" ]; then
    log_warn "Falha ao gerar hash bcrypt — pulei o seed."
    return 0
  fi

  local id now
  id=$(openssl rand -hex 16)
  now=$(date +%s%3N 2>/dev/null || echo "$(($(date +%s) * 1000))")

  systemctl stop "${APP_NAME}" 2>/dev/null || true
  # Insere ou atualiza o admin padrão para as credenciais fixas
  sqlite3 "${db_file}" <<SQL >/dev/null 2>&1
INSERT INTO users (id, email, password_hash, created_at, company_name, cpf, active, display_name)
VALUES ('${id}', '${admin_user}', '${hash}', ${now}, 'WaCalls', '', 1, 'Administrador')
ON CONFLICT(email) DO UPDATE SET password_hash='${hash}', active=1;
INSERT OR IGNORE INTO user_roles (user_id, role)
SELECT id, 'admin' FROM users WHERE email='${admin_user}';
SQL
  systemctl start "${APP_NAME}" 2>/dev/null || true

  # Persiste credenciais
  cat > "${cred_file}" <<EOF
# WaCalls — credenciais do admin padrão (definidas pelo instalador)
usuario=${admin_user}
senha=${admin_pass}
EOF
  chmod 600 "${cred_file}"
  log_ok "Admin padrão configurado: ${admin_user} / ${admin_pass}"
}


# -------------------- Menu --------------------
menu() {
  banner
  local etapa_atual
  etapa_atual=$(carregar_etapa)
  printf "${WHITE} 1) ${GREEN}Instalar WaCalls (nova instalação)${WHITE}\n"
  if [ -f "$ARQUIVO_ETAPAS" ] && [ "$etapa_atual" -gt 0 ]; then
    printf "${WHITE} 2) ${MAGENTA}Continuar instalação de onde parou (etapa ${etapa_atual})${WHITE}\n"
  else
    printf "${WHITE} 2) ${MAGENTA}Continuar instalação (nenhum checkpoint salvo)${WHITE}\n"
  fi
  printf "${WHITE} 3) ${CYAN}Atualizar versão (zip local ou Git)${WHITE}\n"
  printf "${WHITE} 6) ${YELLOW}Ver logs (journalctl -f)${WHITE}\n"
  printf "${WHITE} 0) Sair${WHITE}\n\n"
  read -p "> " opc
  case "$opc" in
    1)
      if [ -f "$ARQUIVO_VARIAVEIS" ] || [ -f "$ARQUIVO_ETAPAS" ]; then
        log_warn "Checkpoint anterior detectado — será descartado para nova instalação."
        rm -f "$ARQUIVO_VARIAVEIS" "$ARQUIVO_ETAPAS"
      fi
      coletar_dados
      executar_instalacao
      ;;
    2)
      if [ ! -f "$ARQUIVO_VARIAVEIS" ] || [ ! -f "$ARQUIVO_ETAPAS" ]; then
        log_err "Nenhum checkpoint encontrado. Use a opção 1 para instalar."
        exit 1
      fi
      carregar_variaveis
      log_info "Retomando instalação a partir da etapa $(carregar_etapa)..."
      executar_instalacao
      ;;
    3) atualizar_versao ;;
    6) journalctl -u "${APP_NAME}" -f ;;
    0) exit 0 ;;
    *) log_err "Opção inválida." ;;
  esac
}

# -------------------- Atualização de versão --------------------
# Atualiza o código do app já instalado usando um ZIP local em /root
# ou clonando/atualizando a partir de um repositório Git informado.
# Preserva dados (data/, media/, .env, wacalls.db*) e rebuilda o frontend
# antes de reiniciar o serviço.
atualizar_versao() {
  banner
  if [ ! -d "${APP_DIR}" ]; then
    log_err "Nada instalado em ${APP_DIR}. Use a opção 1 primeiro."
    return 1
  fi
  printf "${WHITE} 1) A partir de um arquivo .zip em /root${WHITE}\n"
  printf "${WHITE} 2) A partir de um repositório Git${WHITE}\n"
  printf "${WHITE} 0) Voltar${WHITE}\n\n"
  read -p "> " up_opc
  case "$up_opc" in
    1) atualizar_via_zip ;;
    2) atualizar_via_git ;;
    0) return 0 ;;
    *) log_err "Opção inválida." ;;
  esac
}

atualizar_via_zip() {
  local zip_file
  zip_file=$(selecionar_zip_backend_go 2>/dev/null || true)
  if [ -z "${zip_file}" ] || [ ! -f "${zip_file}" ]; then
    log_err "Nenhum .zip válido encontrado em /root."
    log_info "Coloque o pacote (ex: vozzap-chat.zip / wacalls.zip) em /root e rode novamente."
    return 1
  fi
  log_info "Atualizando ${APP_NAME} a partir de: ${zip_file}"
  systemctl stop "${APP_NAME}.service" 2>/dev/null || true
  aplicar_pacote_zip "${zip_file}" "${APP_DIR}" "sim" || { log_err "Falha ao aplicar zip."; return 1; }
  finalizar_atualizacao
}

atualizar_via_git() {
  local repo branch tmpdir
  printf "${WHITE}Repositório Git (ex: https://github.com/raphaelbat/wacalls-chat.git):${WHITE}\n"
  read -p "> " repo
  [ -n "${repo}" ] || { log_err "Repositório obrigatório."; return 1; }
  printf "${WHITE}Branch [main]:${WHITE}\n"
  read -p "> " branch
  branch="${branch:-main}"

  command -v git >/dev/null 2>&1 || { apt-get update -y >/dev/null 2>&1; apt-get install -y git || { log_err "git indisponível."; return 1; }; }
  tmpdir=$(mktemp -d)
  log_info "Clonando ${repo} (branch ${branch}) em ${tmpdir}..."
  if ! git clone --depth=1 -b "${branch}" "${repo}" "${tmpdir}/src" 2>&1 | tail -5; then
    log_err "Falha no git clone."
    rm -rf "${tmpdir}"
    return 1
  fi

  local src_root
  src_root=$(localizar_raiz_backend_go "${tmpdir}/src" || true)
  if [ -z "${src_root}" ]; then
    log_err "Repositório clonado não contém backend Go completo (go.mod + cmd/server)."
    rm -rf "${tmpdir}"
    return 1
  fi

  log_info "Aplicando código do Git em ${APP_DIR}..."
  systemctl stop "${APP_NAME}.service" 2>/dev/null || true

  local preserve_client_flag=""
  local preserve_dist_flag=""
  [ ! -d "${src_root}/client" ] && preserve_client_flag="! -name client"
  [ ! -d "${src_root}/dist" ]   && preserve_dist_flag="! -name dist"
  find "${APP_DIR}" -mindepth 1 -maxdepth 1 \
    ! -name 'data' ! -name 'media' ! -name '.env' \
    ! -name 'wacalls.db' ! -name 'wacalls.db-shm' ! -name 'wacalls.db-wal' \
    ${preserve_client_flag} ${preserve_dist_flag} \
    -exec rm -rf {} + 2>/dev/null || true
  cp -a "${src_root}/." "${APP_DIR}/" || { log_err "Falha ao copiar código do Git."; rm -rf "${tmpdir}"; return 1; }
  rm -rf "${tmpdir}"

  finalizar_atualizacao
}

finalizar_atualizacao() {
  log_info "Rebuild do frontend..."
  instalar_build_frontend "${APP_DIR}" || log_warn "Falha ao buildar frontend — verifique manualmente."

  if projeto_tem_backend_go "${APP_DIR}"; then
    # Garante Go no PATH mesmo em shells não-login (profile.d não é lido).
    export PATH="$PATH:/usr/local/go/bin:$HOME/go/bin"
    if ! command -v go >/dev/null 2>&1; then
      log_warn "Go não encontrado no PATH — instalando via etapa3_go..."
      etapa3_go || log_warn "Falha ao instalar Go automaticamente."
      export PATH="$PATH:/usr/local/go/bin:$HOME/go/bin"
    fi
    if command -v go >/dev/null 2>&1; then
      log_info "Recompilando backend Go ($(go version 2>/dev/null))..."
      (cd "${APP_DIR}" && CGO_ENABLED=0 GO111MODULE=on go build -o "wacalls-server" ./cmd/server) || {
        log_warn "Falha ao compilar backend."
      }
      if [ -x "${APP_DIR}/wacalls-server" ]; then
        chmod +x "${APP_DIR}/wacalls-server" 2>/dev/null || true
      else
        log_warn "Binário ${APP_DIR}/wacalls-server não foi gerado; o serviço não conseguirá iniciar."
      fi
    else
      log_warn "Go indisponível — pulei a recompilação do backend."
    fi
  fi

  garantir_static_dist "${APP_DIR}" 2>/dev/null || true
  if [ -f "/etc/systemd/system/${APP_NAME}.service" ]; then
    sed -i -E "\/Environment=\/?root\/${APP_NAME}=?$/d" "/etc/systemd/system/${APP_NAME}.service" 2>/dev/null || true
    sed -i -E "s|ExecStart=.*${APP_NAME}([[:space:]]|$)|ExecStart=${APP_DIR}/wacalls-server -addr :$(descobrir_porta_app) -static ${APP_DIR}/dist -db ${APP_DIR}/wacalls.db|" "/etc/systemd/system/${APP_NAME}.service" 2>/dev/null || true
  fi
  systemctl daemon-reload >/dev/null 2>&1 || true
  systemctl restart "${APP_NAME}.service" || log_warn "Falha ao reiniciar ${APP_NAME}."
  sleep 2
  if systemctl is-active --quiet "${APP_NAME}.service"; then
    log_ok "Atualização concluída — ${APP_NAME} rodando."
  else
    log_warn "Serviço não subiu. Últimos logs:"
    systemctl --no-pager --full status "${APP_NAME}.service" || true
    journalctl -u "${APP_NAME}" -n 80 --no-pager || true
  fi
}

# -------------------- Whisper Bridge (STT grátis, local) --------------------
# Provisiona um serviço HTTP local (127.0.0.1:5006) que recebe áudio via
# multipart/form-data e devolve {"text": "..."} usando faster-whisper. É
# 100% gratuito, roda na CPU da própria VPS e atende o botão "Transcrever"
# do player de áudio do chat.
etapa_whisper() {
  banner
  log_info "[STT] Instalando Whisper Bridge (faster-whisper, gratuito)..."

  apt-get install -y python3 python3-pip python3-venv ffmpeg >/dev/null 2>&1 || true

  install -d -m 0755 /opt/whisper-bridge
  python3 -m venv /opt/whisper-bridge >/dev/null 2>&1 || true
  /opt/whisper-bridge/bin/pip install --quiet --upgrade pip >/dev/null 2>&1 || true
  /opt/whisper-bridge/bin/pip install --quiet "faster-whisper>=1.0.0" "aiohttp>=3.9" >/dev/null 2>&1 || {
    log_warn "Falha ao instalar faster-whisper — STT ficará indisponível"
    return 0
  }

  # Modelo: 'small' é o melhor custo/qualidade na CPU (≈460 MB). Para máquinas
  # pequenas troque para 'base' editando WHISPER_MODEL no /etc/default/whisper-bridge.
  cat > /etc/default/whisper-bridge <<'ENVEOF'
# Perfil LOW-MEM (padrão): modelo tiny + int8 + 1 thread (~250-400 MB RAM).
# Para mais qualidade troque WHISPER_MODEL para base/small (consome mais RAM).
WHISPER_MODEL=tiny
WHISPER_DEVICE=cpu
WHISPER_COMPUTE=int8
WHISPER_BIND=127.0.0.1
WHISPER_PORT=5006
WHISPER_LANG=pt
WHISPER_THREADS=1
WHISPER_WORKERS=1
WHISPER_BEAM=1
OMP_NUM_THREADS=1
MKL_NUM_THREADS=1
ENVEOF

  cat > /opt/whisper-bridge/bridge.py <<'PYEOF'
#!/usr/bin/env python3
"""HTTP bridge for faster-whisper.

POST /transcribe  (multipart/form-data: file=<audio>, language=<iso, opcional>)
Resposta: {"text": "..."}
"""
import os, tempfile, asyncio, logging
from aiohttp import web
from faster_whisper import WhisperModel

LOG = logging.getLogger("whisper-bridge")
logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")

MODEL = os.environ.get("WHISPER_MODEL", "small")
DEVICE = os.environ.get("WHISPER_DEVICE", "cpu")
COMPUTE = os.environ.get("WHISPER_COMPUTE", "int8")
BIND = os.environ.get("WHISPER_BIND", "127.0.0.1")
PORT = int(os.environ.get("WHISPER_PORT", "5006"))
DEFAULT_LANG = os.environ.get("WHISPER_LANG", "pt") or None

LOG.info("loading model=%s device=%s compute=%s", MODEL, DEVICE, COMPUTE)
THREADS = int(os.environ.get("WHISPER_THREADS", "1"))
WORKERS = int(os.environ.get("WHISPER_WORKERS", "1"))
BEAM = int(os.environ.get("WHISPER_BEAM", "1"))
model = WhisperModel(MODEL, device=DEVICE, compute_type=COMPUTE, cpu_threads=THREADS, num_workers=WORKERS)
LOG.info("model ready")

LOCK = asyncio.Lock()

async def health(_req):
    return web.json_response({"ok": True, "model": MODEL})

async def transcribe(req: web.Request):
    reader = await req.multipart()
    audio_bytes = None
    language = DEFAULT_LANG
    async for part in reader:
        if part.name == "file":
            audio_bytes = await part.read(decode=False)
        elif part.name == "language":
            txt = (await part.text()).strip()
            if txt:
                language = txt
    if not audio_bytes:
        return web.json_response({"error": "file missing"}, status=400)

    with tempfile.NamedTemporaryFile(suffix=".bin", delete=False) as tmp:
        tmp.write(audio_bytes)
        path = tmp.name
    try:
        async with LOCK:
            def run():
                import gc
                segments, _info = model.transcribe(
                    path,
                    language=language,
                    vad_filter=True,
                    beam_size=BEAM,
                    condition_on_previous_text=False,
                )
                out = " ".join(seg.text.strip() for seg in segments).strip()
                gc.collect()
                return out
            text = await asyncio.get_running_loop().run_in_executor(None, run)
    except Exception as exc:
        LOG.exception("transcribe failed")
        return web.json_response({"error": str(exc)}, status=500)
    finally:
        try: os.unlink(path)
        except OSError: pass
    return web.json_response({"text": text})

app = web.Application(client_max_size=64 * 1024 * 1024)
app.router.add_get("/health", health)
app.router.add_post("/transcribe", transcribe)

if __name__ == "__main__":
    web.run_app(app, host=BIND, port=PORT, print=None)
PYEOF
  chmod 0755 /opt/whisper-bridge/bridge.py

  cat > /etc/systemd/system/whisper-bridge.service <<'UNITEOF'
[Unit]
Description=Whisper Bridge (faster-whisper STT HTTP) para o WaCalls
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=-/etc/default/whisper-bridge
ExecStart=/opt/whisper-bridge/bin/python /opt/whisper-bridge/bridge.py
Restart=always
RestartSec=5
Nice=10
CPUQuota=70%
MemoryHigh=600M
MemoryMax=900M
TasksMax=64
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
UNITEOF

  systemctl daemon-reload
  systemctl enable --now whisper-bridge.service >/dev/null 2>&1 || \
    log_warn "Falha ao iniciar whisper-bridge.service (veja: journalctl -u whisper-bridge)"

  log_ok "Whisper Bridge pronto em 127.0.0.1:5006 (POST /transcribe)."
}

carregar_variaveis
menu
