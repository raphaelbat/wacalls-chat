package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func abrirKanban(t *testing.T) (*kanbanStore, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "kanban.db"))
	if err != nil {
		t.Fatalf("abrindo banco: %v", err)
	}
	st, err := newKanbanStore(context.Background(), db)
	if err != nil {
		t.Fatalf("criando store: %v", err)
	}
	return st, db
}

// Vincular a mesma conversa duas vezes ao mesmo quadro nao pode criar cartao
// novo: era o que enchia a coluna de copias e o cabecalho do atendimento de
// selos repetidos.
func TestVincularDuasVezesNaoDuplica(t *testing.T) {
	ctx := context.Background()
	st, db := abrirKanban(t)
	defer db.Close()

	b, err := st.CreateBoard(ctx, "Atendimento", "#25D366", "", "dono")
	if err != nil {
		t.Fatalf("quadro: %v", err)
	}
	col, err := st.CreateColumn(ctx, b.ID, "A fazer", "#888", "open", 4)
	if err != nil {
		t.Fatalf("coluna: %v", err)
	}

	novo := cardCreate{BoardID: b.ID, ColumnID: col.ID, Title: "Equipechat",
		SessionID: "sessao-1", ChatJID: "5581999@s.whatsapp.net"}

	c1, err := st.CreateCard(ctx, novo)
	if err != nil {
		t.Fatalf("primeiro cartao: %v", err)
	}
	c2, err := st.CreateCard(ctx, novo)
	if err != nil {
		t.Fatalf("segundo cartao: %v", err)
	}
	if c1.ID != c2.ID {
		t.Fatalf("criou cartao novo (%s e %s) em vez de reaproveitar", c1.ID, c2.ID)
	}

	// O backfill cria sem a conexao; nao pode virar um terceiro cartao.
	c3, err := st.CreateCard(ctx, cardCreate{BoardID: b.ID, ColumnID: col.ID,
		Title: "Equipechat", ChatJID: "5581999@s.whatsapp.net"})
	if err != nil {
		t.Fatalf("terceiro cartao: %v", err)
	}
	if c3.ID != c1.ID {
		t.Fatal("cartao sem conexao duplicou a mesma conversa")
	}

	cards, err := st.ListCards(ctx, b.ID)
	if err != nil {
		t.Fatalf("listando: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("o quadro ficou com %d cartoes, esperado 1", len(cards))
	}
}

// Mas quadros diferentes continuam podendo acompanhar a mesma conversa:
// Vendas e Suporte olham o mesmo cliente por angulos diferentes.
func TestMesmaConversaEmQuadrosDiferentes(t *testing.T) {
	ctx := context.Background()
	st, db := abrirKanban(t)
	defer db.Close()

	vendas, _ := st.CreateBoard(ctx, "Vendas", "#25D366", "", "dono")
	suporte, _ := st.CreateBoard(ctx, "Suporte", "#F0B357", "", "dono")
	colV, _ := st.CreateColumn(ctx, vendas.ID, "Novo", "#888", "open", 0)
	colS, _ := st.CreateColumn(ctx, suporte.ID, "Aberto", "#888", "open", 0)

	jid := "5581999@s.whatsapp.net"
	cv, err := st.CreateCard(ctx, cardCreate{BoardID: vendas.ID, ColumnID: colV.ID, Title: "Cliente", SessionID: "s1", ChatJID: jid})
	if err != nil {
		t.Fatalf("vendas: %v", err)
	}
	cs, err := st.CreateCard(ctx, cardCreate{BoardID: suporte.ID, ColumnID: colS.ID, Title: "Cliente", SessionID: "s1", ChatJID: jid})
	if err != nil {
		t.Fatalf("suporte: %v", err)
	}
	if cv.ID == cs.ID {
		t.Fatal("o cartao de Vendas foi reaproveitado no quadro de Suporte")
	}
}
