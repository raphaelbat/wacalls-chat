package main

import (
	"encoding/json"
	"testing"
)

// O construtor visual (client/src/components/domain/flow/node-catalog.ts) grava
// o grafo exatamente neste formato. Se alguém mexer no nome de um campo de um
// lado só, é aqui que a diferença aparece — em vez de aparecer num cliente que
// mandou "1" e não recebeu resposta.
//
// A string abaixo foi copiada da saída real do construtor para um fluxo de
// atendimento simples: saudação, menu com dois botões, condição e fila.
const grafoDoConstrutor = `{
  "nodes": [
    {"id":"chat_text_a","type":"chat_text","position":{"x":80,"y":120},
     "data":{"text":"Olá, {{.vars.nome}}! 👋"}},
    {"id":"chat_msg_api_b","type":"chat_msg_api","position":{"x":420,"y":100},
     "data":{"prompt":"Como podemos te ajudar?","footer":"Escolha uma opção","renderAs":"buttons",
             "saveAs":"assunto",
             "options":[{"key":"comprar","label":"Comprar"},{"key":"suporte","label":"Suporte"}]}},
    {"id":"chat_if_else_c","type":"chat_if_else","position":{"x":760,"y":40},
     "data":{"logic":"and","conditions":[{"variable":"vars.assunto","operator":"eq","value":"comprar"}]}},
    {"id":"chat_queue_d","type":"chat_queue","position":{"x":1080,"y":0},
     "data":{"queueId":"fila-vendas"}},
    {"id":"chat_queue_e","type":"chat_queue","position":{"x":1080,"y":160},
     "data":{"queueId":"fila-suporte"}},
    {"id":"chat_random_f","type":"chat_random","position":{"x":760,"y":300},
     "data":{"options":[{"key":"a","label":"A","weight":70},{"key":"b","label":"B","weight":30}]}}
  ],
  "edges": [
    {"id":"e1","source":"chat_text_a","target":"chat_msg_api_b","sourceHandle":""},
    {"id":"e2","source":"chat_msg_api_b","target":"chat_if_else_c","sourceHandle":"comprar"},
    {"id":"e3","source":"chat_msg_api_b","target":"chat_queue_e","sourceHandle":"suporte"},
    {"id":"e4","source":"chat_if_else_c","target":"chat_queue_d","sourceHandle":"true"},
    {"id":"e5","source":"chat_if_else_c","target":"chat_queue_e","sourceHandle":"false"},
    {"id":"e6","source":"chat_random_f","target":"chat_queue_d","sourceHandle":"a"},
    {"id":"e7","source":"chat_random_f","target":"chat_queue_e","sourceHandle":"b"}
  ],
  "startNodeId": "chat_text_a",
  "kind": "chat"
}`

func grafoDeTeste(t *testing.T) FlowGraph {
	t.Helper()
	var g FlowGraph
	if err := json.Unmarshal([]byte(grafoDoConstrutor), &g); err != nil {
		t.Fatalf("o executor não conseguiu ler o grafo do construtor: %v", err)
	}
	return g
}

// Sem kind:"chat" o servidor trata o fluxo como URA de voz e ele nunca dispara
// por mensagem — o erro mais silencioso possível.
func TestGrafoDoConstrutorEhReconhecidoComoChat(t *testing.T) {
	if got := flowKind(&FlowRow{Graph: grafoDoConstrutor}); got != "chat" {
		t.Fatalf("flowKind = %q, queria \"chat\"", got)
	}
}

func TestGrafoDoConstrutorTemNosEArestas(t *testing.T) {
	g := grafoDeTeste(t)
	if len(g.Nodes) != 6 || len(g.Edges) != 7 {
		t.Fatalf("li %d nós e %d arestas", len(g.Nodes), len(g.Edges))
	}
	if g.StartNodeID != "chat_text_a" {
		t.Fatalf("startNodeId = %q", g.StartNodeID)
	}
}

// Os handles que o construtor desenha precisam ser os mesmos que o executor
// procura, senão a opção clicada leva para o lugar errado (ou para lugar nenhum).
func TestHandlesDoConstrutorCasamComOExecutor(t *testing.T) {
	g := grafoDeTeste(t)
	casos := []struct {
		nome, no, handle, esperado string
	}{
		{"saída única segue pela aresta sem handle", "chat_text_a", "", "chat_msg_api_b"},
		{"opção do menu usa a key como handle", "chat_msg_api_b", "comprar", "chat_if_else_c"},
		{"segunda opção do menu", "chat_msg_api_b", "suporte", "chat_queue_e"},
		{"condição verdadeira", "chat_if_else_c", "true", "chat_queue_d"},
		{"condição falsa", "chat_if_else_c", "false", "chat_queue_e"},
		{"sorteio usa a key da opção", "chat_random_f", "a", "chat_queue_d"},
		{"sorteio, outro caminho", "chat_random_f", "b", "chat_queue_e"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := flowNextOf(g, c.no, c.handle); got != c.esperado {
				t.Fatalf("de %s pelo handle %q foi para %q, queria %q", c.no, c.handle, got, c.esperado)
			}
		})
	}
}

// O cliente pode responder clicando no botão (vem a key), digitando o texto do
// botão, ou digitando o número da posição. Os três têm que cair no mesmo lugar.
func TestRespostaDoClienteCasaComAOpcao(t *testing.T) {
	g := grafoDeTeste(t)
	var menu FlowNode
	for _, n := range g.Nodes {
		if n.ID == "chat_msg_api_b" {
			menu = n
		}
	}
	for _, resposta := range []string{"comprar", "Comprar", "1"} {
		t.Run(resposta, func(t *testing.T) {
			handle := matchChatOptionBranch(menu, resposta)
			if got := flowNextOf(g, menu.ID, handle); got != "chat_if_else_c" {
				t.Fatalf("respondendo %q o fluxo foi para %q", resposta, got)
			}
		})
	}
}

// Os operadores oferecidos no inspetor precisam existir no executor. Um
// operador desconhecido devolve false calado, e o fluxo desvia sem avisar.
func TestOperadoresDoInspetorExistemNoExecutor(t *testing.T) {
	casos := []struct {
		op, got, val string
		esperado     bool
	}{
		{"eq", "comprar", "comprar", true},
		{"eq", "comprar", "suporte", false},
		{"neq", "comprar", "suporte", true},
		{"contains", "quero comprar hoje", "comprar", true},
		{"starts_with", "comprar agora", "comprar", true},
		{"empty", "", "", true},
		{"not_empty", "algo", "", true},
	}
	for _, c := range casos {
		if got := evalCondition(c.got, c.op, c.val); got != c.esperado {
			t.Errorf("evalCondition(%q, %q, %q) = %v", c.got, c.op, c.val, got)
		}
	}
}

// O caminho de variável que o inspetor sugere ("vars.assunto", "message.body")
// tem que ser o que o lookupVar do executor entende.
func TestCaminhosDeVariavelDoInspetor(t *testing.T) {
	vars := map[string]interface{}{
		"vars":    map[string]interface{}{"assunto": "comprar"},
		"message": map[string]interface{}{"body": "oi"},
	}
	if got := lookupVar(vars, "vars.assunto"); got != "comprar" {
		t.Fatalf("vars.assunto = %q", got)
	}
	if got := lookupVar(vars, "message.body"); got != "oi" {
		t.Fatalf("message.body = %q", got)
	}
	if got := lookupVar(vars, "vars.naoexiste"); got != "" {
		t.Fatalf("variável ausente devolveu %q, queria vazio", got)
	}
}

// A sintaxe de template que o inspetor ensina precisa render de verdade.
func TestSintaxeDeTemplateDoInspetor(t *testing.T) {
	vars := map[string]interface{}{
		"vars":    map[string]interface{}{"nome": "Maria"},
		"message": map[string]interface{}{"body": "oi"},
	}
	if got := renderTemplate("Olá, {{.vars.nome}}!", vars); got != "Olá, Maria!" {
		t.Fatalf("renderTemplate devolveu %q", got)
	}
	if got := renderTemplate("Você disse: {{.message.body}}", vars); got != "Você disse: oi" {
		t.Fatalf("renderTemplate devolveu %q", got)
	}
}
