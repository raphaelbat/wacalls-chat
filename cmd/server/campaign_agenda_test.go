package main

import (
	"testing"
	"time"
)

// Campanha agendada nao dispara antes da hora — e nao fica dormindo horas
// quando a data e longe, senao mudar a data no meio nao teria efeito.
func TestAguardandoAgendamento(t *testing.T) {
	agora := time.Date(2026, 9, 4, 10, 0, 0, 0, time.Local)

	casos := []struct {
		nome     string
		startAt  int64
		espera   bool
		olharEm  time.Duration
	}{
		{"sem agendamento", 0, false, 0},
		{"marcada para daqui a 2 minutos", agora.Add(2 * time.Minute).Unix(), true, 2 * time.Minute},
		{"marcada para daqui a 3 dias", agora.Add(72 * time.Hour).Unix(), true, 5 * time.Minute},
		{"marcada para agora", agora.Unix(), false, 0},
		{"marcada para ontem", agora.Add(-24 * time.Hour).Unix(), false, 0},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			esperando, falta := aguardandoAgendamento(CampaignRow{StartAt: caso.startAt}, agora)
			if esperando != caso.espera {
				t.Fatalf("esperando=%v, esperado %v", esperando, caso.espera)
			}
			if !caso.espera {
				return
			}
			// A janela de nova conferencia nao pode passar de 5 min nem sumir.
			if falta > 5*time.Minute || falta < time.Second {
				t.Fatalf("proxima conferencia fora do razoavel: %v", falta)
			}
			if caso.olharEm <= 5*time.Minute {
				diff := falta - caso.olharEm
				if diff < -2*time.Second || diff > 2*time.Second {
					t.Fatalf("proxima conferencia = %v, esperado ~%v", falta, caso.olharEm)
				}
			}
		})
	}
}
