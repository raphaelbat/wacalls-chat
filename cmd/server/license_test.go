package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Monta uma licença assinada de verdade, do jeito que o Worker emite.
func licencaDeTeste(t *testing.T, pub ed25519.PublicKey, priv ed25519.PrivateKey, fp string) string {
	t.Helper()
	return licencaComValidade(t, pub, priv, fp, time.Now().Add(33*24*time.Hour).Unix())
}

func licencaComValidade(t *testing.T, pub ed25519.PublicKey, priv ed25519.PrivateKey, fp string, expiraEm int64) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"v":           1,
		"codigo":      "WACL-TEST-TEST-TEST",
		"fingerprint": fp,
		"cliente":     "Cliente de Teste",
		"plano":       "mensal",
		"emitidoEm":   time.Now().Unix(),
		"expiraEm":    expiraEm,
	})
	if err != nil {
		t.Fatalf("montando payload: %v", err)
	}
	corpo := base64.RawURLEncoding.EncodeToString(payload)
	sig := ed25519.Sign(priv, []byte(corpo))
	return "WACL1." + corpo + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func chaves(t *testing.T) (string, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("gerando chaves: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(pub), pub, priv
}

// O PowerShell 5.1 grava UTF-8 COM BOM por padrão. O BOM não é espaço para o
// strings.TrimSpace e o json.Unmarshal o rejeita — na prática o serviço recusava
// uma licença perfeitamente válida com "formato de licenca desconhecido".
func TestVerifyLicenseAceitaJSONComBOM(t *testing.T) {
	pubB64, pub, priv := chaves(t)
	fp := machineFingerprint()
	lic := licencaDeTeste(t, pub, priv, fp)

	arquivo, err := json.Marshal(map[string]string{
		"licenca":     lic,
		"codigo":      "WACL-TEST-TEST-TEST",
		"fingerprint": fp,
	})
	if err != nil {
		t.Fatalf("montando licenca.json: %v", err)
	}

	for _, caso := range []struct {
		nome    string
		prefixo []byte
	}{
		{"sem BOM", nil},
		{"com BOM", []byte{0xEF, 0xBB, 0xBF}},
		{"com BOM e quebra de linha", []byte{0xEF, 0xBB, 0xBF, '\r', '\n'}},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			caminho := filepath.Join(t.TempDir(), "licenca.json")
			if err := os.WriteFile(caminho, append(caso.prefixo, arquivo...), 0o600); err != nil {
				t.Fatalf("gravando: %v", err)
			}
			dados, err := verifyLicense(caminho, pubB64)
			if err != nil {
				t.Fatalf("licenca valida foi recusada: %v", err)
			}
			if dados.Codigo != "WACL-TEST-TEST-TEST" {
				t.Fatalf("codigo lido = %q", dados.Codigo)
			}
		})
	}
}

// A licença crua (sem o JSON em volta) também é aceita, com ou sem BOM.
func TestVerifyLicenseAceitaLicencaCrua(t *testing.T) {
	pubB64, pub, priv := chaves(t)
	lic := licencaDeTeste(t, pub, priv, machineFingerprint())

	caminho := filepath.Join(t.TempDir(), "licenca.txt")
	if err := os.WriteFile(caminho, append([]byte{0xEF, 0xBB, 0xBF}, []byte(lic+"\r\n")...), 0o600); err != nil {
		t.Fatalf("gravando: %v", err)
	}
	if _, err := verifyLicense(caminho, pubB64); err != nil {
		t.Fatalf("licenca crua recusada: %v", err)
	}
}

// Uma licença assinada por outra chave não pode passar - é o que impede alguém
// de emitir as proprias licenças.
func TestVerifyLicenseRecusaOutroEmissor(t *testing.T) {
	pubB64, _, _ := chaves(t)
	_, outroPub, outroPriv := chaves(t)
	lic := licencaDeTeste(t, outroPub, outroPriv, machineFingerprint())

	caminho := filepath.Join(t.TempDir(), "licenca.txt")
	if err := os.WriteFile(caminho, []byte(lic), 0o600); err != nil {
		t.Fatalf("gravando: %v", err)
	}
	_, err := verifyLicense(caminho, pubB64)
	if err == nil {
		t.Fatal("licenca de outro emissor foi aceita")
	}
	if !strings.Contains(err.Error(), "assinatura invalida") {
		t.Fatalf("erro inesperado: %v", err)
	}
}

// Licença emitida para outra máquina não vale nesta.
func TestVerifyLicenseRecusaOutraMaquina(t *testing.T) {
	pubB64, pub, priv := chaves(t)
	lic := licencaDeTeste(t, pub, priv, "00000000000000000000000000000000")

	caminho := filepath.Join(t.TempDir(), "licenca.txt")
	if err := os.WriteFile(caminho, []byte(lic), 0o600); err != nil {
		t.Fatalf("gravando: %v", err)
	}
	if _, err := verifyLicense(caminho, pubB64); err == nil {
		t.Fatal("licenca de outra maquina foi aceita")
	}
}

// A faixa de aviso do painel vive deste estado. O que importa aqui é a
// fronteira: 7 dias de tolerância depois do vencimento o sistema ainda roda,
// e no oitavo não roda mais — é exatamente o que o cliente vê.
func TestLicenseStatusFaixasDeVencimento(t *testing.T) {
	pubB64, pub, priv := chaves(t)
	t.Setenv("WACALLS_LICENSE_PUBKEY", pubB64)
	t.Setenv("WACALLS_LICENSE_REQUIRED", "1")

	dia := int64(24 * 60 * 60)
	casos := []struct {
		nome         string
		expiraEm     int64
		valida       bool
		tolerancia   bool
		diasEsperado int
	}{
		{"com folga", time.Now().Unix() + 20*dia, true, false, 20},
		{"vence em 2 dias", time.Now().Unix() + 2*dia, true, false, 2},
		{"venceu ha 3 dias", time.Now().Unix() - 3*dia, true, true, -3},
		{"venceu ha 10 dias", time.Now().Unix() - 10*dia, false, false, -10},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			caminho := filepath.Join(t.TempDir(), "licenca.json")
			lic := licencaComValidade(t, pub, priv, machineFingerprint(), c.expiraEm)
			if err := os.WriteFile(caminho, []byte(lic), 0o600); err != nil {
				t.Fatalf("gravando: %v", err)
			}
			antes := licenseFlagPath
			licenseFlagPath = caminho
			defer func() { licenseFlagPath = antes }()

			st := licenseStatus()
			if !st.Exigida {
				t.Fatal("a licenca deveria estar exigida")
			}
			if st.Valida != c.valida {
				t.Fatalf("valida = %v, esperado %v (motivo: %s)", st.Valida, c.valida, st.Motivo)
			}
			if st.EmTolerancia != c.tolerancia {
				t.Fatalf("emTolerancia = %v, esperado %v", st.EmTolerancia, c.tolerancia)
			}
			if st.DiasParaVencer != c.diasEsperado {
				t.Fatalf("diasParaVencer = %d, esperado %d", st.DiasParaVencer, c.diasEsperado)
			}
			if c.tolerancia && st.DiasTolerancia != licenseGraceDays+c.diasEsperado {
				t.Fatalf("diasTolerancia = %d", st.DiasTolerancia)
			}
			if st.Suporte != licenseSupportPhone {
				t.Fatalf("suporte = %q", st.Suporte)
			}
		})
	}
}

// Sem arquivo nenhum a API não pode explodir nem mentir que está válida.
func TestLicenseStatusSemArquivo(t *testing.T) {
	t.Setenv("WACALLS_LICENSE_REQUIRED", "0")
	antes := licenseFlagPath
	licenseFlagPath = filepath.Join(t.TempDir(), "nao-existe.json")
	defer func() { licenseFlagPath = antes }()

	// Sem arquivo o licensePath cai na pasta de trabalho; garante que ali
	// tambem nao ha licenca.
	if _, err := os.Stat("licenca.json"); err == nil {
		t.Skip("ha um licenca.json na pasta do teste")
	}
	st := licenseStatus()
	if st.Valida {
		t.Fatal("sem arquivo a licenca nao pode ser valida")
	}
	if st.Motivo == "" {
		t.Fatal("deveria explicar o motivo")
	}
}
