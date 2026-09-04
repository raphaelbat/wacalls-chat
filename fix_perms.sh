#!/bin/bash

# Este script deve ser executado pelo usuário no terminal do servidor (root)
# para garantir que o usuário Super Admin tenha as permissões corretas no banco.

DB_FILE="/root/wacalls/wacalls.db"

if [ ! -f "$DB_FILE" ]; then
    # Tenta localizar o banco se não estiver no caminho padrão
    DB_FILE=$(find /root -name "wacalls.db" | head -n 1)
fi

if [ -z "$DB_FILE" ]; then
    echo "Erro: Arquivo wacalls.db não encontrado."
    exit 1
fi

echo "Usando banco de dados: $DB_FILE"

sqlite3 "$DB_FILE" <<EOF
-- Garante que a tabela users existe
CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, email TEXT UNIQUE, password_hash TEXT, created_at INTEGER, company_name TEXT, cpf TEXT, active INTEGER, display_name TEXT);
CREATE TABLE IF NOT EXISTS user_roles (user_id TEXT, role TEXT, PRIMARY KEY(user_id, role));

-- Insere ou atualiza o usuário admin@wacalls.com.br
INSERT OR IGNORE INTO users (id, email, password_hash, created_at, company_name, cpf, active, display_name) 
VALUES ('admin-wacalls-br', 'admin@wacalls.com.br', '\$2a\$12\$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/aWnC2Wqx0W9q2W9q2', strftime('%s','now'), 'WaCalls', '00000000000', 1, 'Super Admin');

-- Insere ou atualiza o usuário admin@wacalls.com
INSERT OR IGNORE INTO users (id, email, password_hash, created_at, company_name, cpf, active, display_name) 
VALUES ('admin-wacalls-global', 'admin@wacalls.com', '\$2a\$12\$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/aWnC2Wqx0W9q2W9q2', strftime('%s','now'), 'WaCalls', '00000000000', 1, 'Super Admin');

-- Garante cargo admin para ambos
INSERT OR IGNORE INTO user_roles (user_id, role) VALUES ('admin-wacalls-br', 'admin');
INSERT OR IGNORE INTO user_roles (user_id, role) VALUES ('admin-wacalls-global', 'admin');

-- Força a senha admin123456 caso já existam
UPDATE users SET password_hash = '\$2a\$12\$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/aWnC2Wqx0W9q2W9q2' WHERE email IN ('admin@wacalls.com.br', 'admin@wacalls.com');
EOF

echo "Permissões aplicadas com sucesso para admin@wacalls.com.br e admin@wacalls.com."
echo "Senha definida como: admin123456"
