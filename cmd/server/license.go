package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Licença de uso (opcional)
//
// A licença é emitida pelo servidor de ativação (licencas/worker) e validada
// AQUI, offline, com a chave pública. Formato:
//
//     WACL1.<payload base64url>.<assinatura Ed25519 base64url>
//
// O arquivo licenca.json fica ao lado do binário (ou onde -license apontar) e
// é gravado pelo instalador no momento da ativação.
//
// Nada disso liga sozinho: só entra em ação quando WACALLS_LICENSE_REQUIRED=1.
// Assim a versão livre deste repositório continua rodando sem trava alguma, e
// quem distribui a versão paga habilita a exigência na hora de empacotar.
// ---------------------------------------------------------------------------

type licenseData struct {
	V           int    `json:"v"`
	Codigo      string `json:"codigo"`
	Fingerprint string `json:"fingerprint"`
	Cliente     string `json:"cliente"`
	Email       string `json:"email"`
	Plano       string `json:"plano"`
	Pedido      string `json:"pedido"`
	EmitidoEm   int64  `json:"emitidoEm"`
	ExpiraEm    int64  `json:"expiraEm"` // 0 = vitalícia
}

type licenseFile struct {
	Licenca   string `json:"licenca"`
	Codigo    string `json:"codigo,omitempty"`
	AtivadaEm string `json:"ativadaEm,omitempty"`
}

var (
	errNoLicense      = errors.New("nenhuma licenca encontrada")
	errLicenseExpired = errors.New("assinatura vencida")
)

// Configuracao gravada no binario na hora de compilar:
//
//	go build -ldflags "-X main.licenseRequiredBuild=1 -X main.licensePubKeyBuild=<chave> -X main.licenseServerBuild=<url>"
//
// Vale mais que as variaveis de ambiente de proposito: assim o cliente nao
// desliga a exigencia editando o .env. Vazio (build da versao livre) = sem
// trava, e o .env volta a mandar.
var (
	licenseRequiredBuild string
	licensePubKeyBuild   string
	licenseServerBuild   string
)

// Dias que o sistema continua funcionando depois do vencimento. Serve para
// nao derrubar quem esta pagando quando a internet cai ou a cobranca atrasa
// um pouco no cartao.
const licenseGraceDays = 7

// Comeca a tentar renovar quando faltar isto para vencer.
const licenseRenewWindowDays = 7

func licenseServerURL() string {
	if licenseServerBuild != "" {
		return strings.TrimRight(strings.TrimSpace(licenseServerBuild), "/")
	}
	return strings.TrimRight(strings.TrimSpace(os.Getenv("WACALLS_LICENSE_SERVER")), "/")
}

// licensePubKey: a chave gravada no binario vence a do ambiente.
func licensePubKey() string {
	if licensePubKeyBuild != "" {
		return strings.TrimSpace(licensePubKeyBuild)
	}
	return strings.TrimSpace(os.Getenv("WACALLS_LICENSE_PUBKEY"))
}

func licenseRequired() bool {
	if licenseRequiredBuild != "" {
		switch strings.ToLower(strings.TrimSpace(licenseRequiredBuild)) {
		case "1", "on", "true", "sim", "yes":
			return true
		default:
			return false
		}
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WACALLS_LICENSE_REQUIRED"))) {
	case "1", "on", "true", "sim", "yes":
		return true
	}
	return false
}

// licensePath resolve onde procurar o licenca.json: o valor de -license, a
// variável WACALLS_LICENSE_FILE, a pasta de trabalho, ou a pasta do binário.
func licensePath(flagValue string) string {
	candidatos := []string{flagValue, os.Getenv("WACALLS_LICENSE_FILE"), "licenca.json"}
	if exe, err := os.Executable(); err == nil {
		candidatos = append(candidatos, filepath.Join(filepath.Dir(exe), "licenca.json"))
	}
	for _, c := range candidatos {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func b64urlDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

// machineFingerprint identifica o computador. Precisa bater EXATAMENTE com o
// que o instalador calcula, senão a licença ativada não vale aqui:
//
//	sha256("wacalls:" + <id da maquina em minusculas>) -> 32 primeiros hex
//
// Windows: MachineGuid do registro. Linux: /etc/machine-id. Sem os dois,
// cai no hostname — pior, mas melhor do que travar quem é legítimo.
func machineFingerprint() string {
	id := ""
	switch runtime.GOOS {
	case "windows":
		// O "/reg:64" não é detalhe: o instalador do NSIS roda em 32 bits e,
		// sem forçar a visão do registro, um lado lê WOW6432Node e o outro lê
		// a chave real — são dois MachineGuid diferentes, e a licença ativada
		// não vale na mesma máquina que acabou de ativá-la.
		out, err := exec.Command("reg", "query",
			`HKLM\SOFTWARE\Microsoft\Cryptography`, "/v", "MachineGuid", "/reg:64").Output()
		if err != nil {
			// Windows de 32 bits não conhece /reg:64.
			out, err = exec.Command("reg", "query",
				`HKLM\SOFTWARE\Microsoft\Cryptography`, "/v", "MachineGuid").Output()
		}
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if !strings.Contains(line, "MachineGuid") {
					continue
				}
				campos := strings.Fields(strings.TrimSpace(line))
				if len(campos) > 0 {
					id = campos[len(campos)-1]
				}
			}
		}
	default:
		for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
			if b, err := os.ReadFile(p); err == nil {
				id = strings.TrimSpace(string(b))
				break
			}
		}
	}
	if id == "" {
		id, _ = os.Hostname()
	}
	sum := sha256.Sum256([]byte("wacalls:" + strings.ToLower(strings.TrimSpace(id))))
	return hex.EncodeToString(sum[:])[:32]
}

// verifyLicense confere assinatura, máquina e validade.
func verifyLicense(path, pubKeyB64 string) (licenseData, error) {
	var vazio licenseData
	if path == "" {
		return vazio, errNoLicense
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return vazio, errNoLicense
	}

	// O PowerShell grava UTF-8 com BOM por padrão, e o BOM não é espaço para o
	// strings.TrimSpace nem é aceito pelo json.Unmarshal — o arquivo certo era
	// recusado como "formato desconhecido". Fora daqui ninguém percebe.
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})

	// Aceita tanto o JSON gravado pelo instalador quanto a licença crua.
	texto := strings.TrimSpace(string(raw))
	if strings.HasPrefix(texto, "{") {
		var lf licenseFile
		if err := json.Unmarshal([]byte(texto), &lf); err != nil {
			return vazio, fmt.Errorf("licenca.json ilegivel: %w", err)
		}
		texto = strings.TrimSpace(lf.Licenca)
	}

	partes := strings.Split(texto, ".")
	if len(partes) != 3 || partes[0] != "WACL1" {
		return vazio, errors.New("formato de licenca desconhecido")
	}
	pub, err := b64urlDecode(strings.TrimSpace(pubKeyB64))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return vazio, errors.New("chave publica de licenca invalida (WACALLS_LICENSE_PUBKEY)")
	}
	assinatura, err := b64urlDecode(partes[2])
	if err != nil {
		return vazio, errors.New("assinatura ilegivel")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(partes[1]), assinatura) {
		return vazio, errors.New("assinatura invalida (licenca adulterada ou de outro emissor)")
	}

	payload, err := b64urlDecode(partes[1])
	if err != nil {
		return vazio, errors.New("conteudo da licenca ilegivel")
	}
	var dados licenseData
	if err := json.Unmarshal(payload, &dados); err != nil {
		return vazio, errors.New("conteudo da licenca ilegivel")
	}
	if dados.ExpiraEm > 0 && time.Now().Unix() > dados.ExpiraEm {
		return dados, fmt.Errorf("%w em %s", errLicenseExpired, time.Unix(dados.ExpiraEm, 0).Format("02/01/2006"))
	}
	if fp := machineFingerprint(); dados.Fingerprint != "" && dados.Fingerprint != fp {
		return dados, errors.New("licenca ativada em outro computador")
	}
	return dados, nil
}

type licenseLogger interface {
	Info(string, ...any)
	Warn(string, ...any)
	Error(string, ...any)
}

// renewLicense troca a licença atual por uma nova no servidor de ativação.
// É o que renova a mensalidade sem o cliente digitar nada: enquanto a
// assinatura estiver paga, o servidor devolve licença com validade nova.
func renewLicense(path string, atual licenseData) (licenseData, error) {
	servidor := licenseServerURL()
	if servidor == "" {
		return atual, errors.New("WACALLS_LICENSE_SERVER nao configurado")
	}
	if atual.Codigo == "" {
		return atual, errors.New("licenca sem codigo")
	}

	corpo, _ := json.Marshal(map[string]string{
		"codigo":      atual.Codigo,
		"fingerprint": machineFingerprint(),
	})
	cli := &http.Client{Timeout: 20 * time.Second}
	resp, err := cli.Post(servidor+"/api/renovar", "application/json", bytes.NewReader(corpo))
	if err != nil {
		return atual, err
	}
	defer resp.Body.Close()

	var out struct {
		OK      bool   `json:"ok"`
		Licenca string `json:"licenca"`
		Erro    string `json:"erro"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return atual, fmt.Errorf("resposta ilegivel do servidor de licencas: %w", err)
	}
	if !out.OK || out.Licenca == "" {
		if out.Erro != "" {
			return atual, errors.New(out.Erro)
		}
		return atual, fmt.Errorf("servidor recusou a renovacao (HTTP %d)", resp.StatusCode)
	}

	arquivo := licenseFile{
		Licenca:   out.Licenca,
		Codigo:    atual.Codigo,
		AtivadaEm: time.Now().Format("2006-01-02 15:04:05"),
	}
	dadosArquivo, _ := json.MarshalIndent(arquivo, "", "  ")
	destino := path
	if destino == "" {
		if exe, err := os.Executable(); err == nil {
			destino = filepath.Join(filepath.Dir(exe), "licenca.json")
		} else {
			destino = "licenca.json"
		}
	}
	if err := os.WriteFile(destino, dadosArquivo, 0o600); err != nil {
		return atual, fmt.Errorf("nao consegui gravar %s: %w", destino, err)
	}
	return verifyLicense(destino, licensePubKey())
}

// diasParaVencer devolve quantos dias faltam (negativo = já venceu).
func diasParaVencer(d licenseData) int {
	if d.ExpiraEm <= 0 {
		return 1 << 30 // vitalícia
	}
	return int((d.ExpiraEm - time.Now().Unix()) / 86400)
}

// startLicenseWatcher renova a mensalidade em segundo plano. Roda a cada 12h e
// só conversa com o servidor quando falta pouco para vencer.
func startLicenseWatcher(path, pubKey string, log licenseLogger) {
	if licenseServerURL() == "" {
		return
	}
	go func() {
		for {
			time.Sleep(12 * time.Hour)
			dados, err := verifyLicense(licensePath(path), pubKey)
			if err != nil && !errors.Is(err, errLicenseExpired) {
				continue
			}
			if diasParaVencer(dados) > licenseRenewWindowDays {
				continue
			}
			novo, rerr := renewLicense(licensePath(path), dados)
			if rerr == nil {
				log.Info("licenca renovada", "validade", time.Unix(novo.ExpiraEm, 0).Format("02/01/2006"))
				continue
			}
			log.Warn("nao consegui renovar a licenca", "motivo", rerr, "dias_para_vencer", diasParaVencer(dados))
			if licenseRequired() && diasParaVencer(dados) < -licenseGraceDays {
				log.Error("assinatura vencida ha mais de 7 dias; encerrando",
					"codigo", dados.Codigo, "maquina", machineFingerprint())
				os.Exit(2)
			}
		}
	}()
}

// checkLicense roda no start. Só derruba o servidor quando a exigência está
// ligada; caso contrário apenas registra o que encontrou.
func checkLicense(flagPath, pubKey string, log licenseLogger) {
	path := licensePath(flagPath)
	dados, err := verifyLicense(path, pubKey)

	// Vencida: tenta renovar na hora antes de reclamar. É o caminho normal de
	// quem paga em dia e ficou uns dias com o computador desligado.
	if errors.Is(err, errLicenseExpired) || (err == nil && diasParaVencer(dados) <= licenseRenewWindowDays) {
		if novo, rerr := renewLicense(path, dados); rerr == nil {
			dados, err = novo, nil
			log.Info("licenca renovada no start", "validade", time.Unix(dados.ExpiraEm, 0).Format("02/01/2006"))
		} else if errors.Is(err, errLicenseExpired) {
			log.Warn("nao consegui renovar a licenca", "motivo", rerr)
		}
	}

	if err == nil {
		validade := "vitalicia"
		if dados.ExpiraEm > 0 {
			validade = time.Unix(dados.ExpiraEm, 0).Format("02/01/2006")
		}
		log.Info("licenca valida", "cliente", dados.Cliente, "codigo", dados.Codigo,
			"plano", dados.Plano, "validade", validade)
		startLicenseWatcher(flagPath, pubKey, log)
		return
	}

	// Tolerância: vencida há poucos dias, o sistema continua funcionando.
	if errors.Is(err, errLicenseExpired) && diasParaVencer(dados) >= -licenseGraceDays {
		log.Warn("assinatura vencida; funcionando em tolerancia",
			"venceu_em", time.Unix(dados.ExpiraEm, 0).Format("02/01/2006"),
			"dias_restantes_de_tolerancia", licenseGraceDays+diasParaVencer(dados))
		startLicenseWatcher(flagPath, pubKey, log)
		return
	}

	if !licenseRequired() {
		if errors.Is(err, errNoLicense) {
			log.Info("rodando sem licenca (versao livre)")
		} else {
			log.Warn("licenca ignorada", "motivo", err)
		}
		return
	}

	log.Error("licenca invalida; o servidor nao vai subir", "arquivo", path, "motivo", err,
		"maquina", machineFingerprint())
	fmt.Fprintf(os.Stderr, `
============================================================
  WaCalls - ativacao necessaria
============================================================
  %v

  Codigo desta maquina: %s

  - Assinatura vencida? Refaca o pagamento na Kiwify: assim que
    a cobranca aprovar, o sistema se reativa sozinho.
  - Primeira instalacao? Rode o instalador e informe o codigo
    de ativacao que voce recebeu por e-mail.

  Suporte: 81 99588-5670
============================================================
`, err, machineFingerprint())
	// Espera antes de sair para o servico nao entrar em ciclo de reinicio rapido.
	time.Sleep(30 * time.Second)
	os.Exit(2)
}

// printLicenseInfo atende o -license-info: mostra o codigo da maquina (que o
// suporte pede para liberar uma troca de PC) e o estado da licenca atual.
func printLicenseInfo(flagPath string) {
	path := licensePath(flagPath)
	fmt.Println("codigo desta maquina:", machineFingerprint())
	if path == "" {
		fmt.Println("licenca            : nenhuma encontrada")
		return
	}
	fmt.Println("arquivo            :", path)
	dados, err := verifyLicense(path, licensePubKey())
	if err != nil {
		fmt.Println("licenca            : INVALIDA -", err)
		return
	}
	validade := "vitalicia"
	if dados.ExpiraEm > 0 {
		validade = time.Unix(dados.ExpiraEm, 0).Format("02/01/2006")
	}
	fmt.Printf("licenca            : valida\ncliente            : %s\ncodigo             : %s\nplano              : %s\nvalidade           : %s\n",
		dados.Cliente, dados.Codigo, dados.Plano, validade)
}

// ---------------------------------------------------------------------------
// Estado da licenca para o painel
// ---------------------------------------------------------------------------

// licenseFlagPath guarda o -license recebido no start, para que a API consiga
// reencontrar o mesmo arquivo que o checkLicense usou.
var licenseFlagPath string

// licenseStatusView e o que o painel mostra na faixa de aviso. Nao leva e-mail
// nem fingerprint: e uma tela que qualquer atendente pode abrir.
type licenseStatusView struct {
	Exigida        bool   `json:"exigida"`
	Valida         bool   `json:"valida"`
	Vitalicia      bool   `json:"vitalicia"`
	Codigo         string `json:"codigo,omitempty"`
	Plano          string `json:"plano,omitempty"`
	ExpiraEm       int64  `json:"expiraEm,omitempty"` // epoch em segundos; 0 = sem validade
	DiasParaVencer int    `json:"diasParaVencer"`
	EmTolerancia   bool   `json:"emTolerancia"`
	DiasTolerancia int    `json:"diasTolerancia,omitempty"`
	Motivo         string `json:"motivo,omitempty"`
	Suporte        string `json:"suporte"`
}

const licenseSupportPhone = "81 99588-5670"

// licenseStatus le o licenca.json de novo a cada chamada. E barato (um arquivo
// pequeno e uma verificacao Ed25519) e evita mostrar um estado velho logo
// depois de o watcher renovar em segundo plano.
func licenseStatus() licenseStatusView {
	v := licenseStatusView{Exigida: licenseRequired(), Suporte: licenseSupportPhone}
	path := licensePath(licenseFlagPath)
	if path == "" {
		v.Motivo = "nenhuma licenca instalada"
		return v
	}
	dados, err := verifyLicense(path, licensePubKey())
	if err != nil && !errors.Is(err, errLicenseExpired) {
		v.Motivo = err.Error()
		return v
	}
	v.Codigo = dados.Codigo
	v.Plano = dados.Plano
	v.ExpiraEm = dados.ExpiraEm
	v.Vitalicia = dados.ExpiraEm <= 0
	if v.Vitalicia {
		v.Valida = true
		v.DiasParaVencer = 1 << 30
		return v
	}
	v.DiasParaVencer = diasParaVencer(dados)
	if err == nil {
		v.Valida = true
		return v
	}
	// Vencida: ainda vale enquanto durar a tolerancia.
	v.Motivo = err.Error()
	if v.DiasParaVencer >= -licenseGraceDays {
		v.Valida = true
		v.EmTolerancia = true
		v.DiasTolerancia = licenseGraceDays + v.DiasParaVencer
	}
	return v
}

func (s *server) handleLicenseStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, licenseStatus())
}
