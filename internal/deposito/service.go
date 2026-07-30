package deposito

import (
	"context"
	"errors"
	"fmt"

	"github.com/Gabox99/pam/internal/blnk"
	"github.com/Gabox99/pam/internal/db"

	"github.com/jackc/pgx/v5"
)

// ErrContaNaoEncontrada indica que o usuário não tem conta (depósito rejeitado)
var ErrContaNaoEncontrada = errors.New("usuario sem conta")

type Service struct {
	queries    *db.Queries
	blnkClient *blnk.Client
}

func New(queries *db.Queries, blnkClient *blnk.Client) *Service {
	return &Service{queries: queries, blnkClient: blnkClient}
}

// ProcessarDeposito credita um depósito no balance do usuário.
// Retorna ErrContaNaoEncontrada se o usuário não tem conta.
func (s *Service) ProcessarDeposito(ctx context.Context, depositoID, userID string, valor int64, moeda string) error {
	// 1. busca a conta do usuário
	conta, err := s.queries.BuscarContaPorUsuario(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrContaNaoEncontrada
	}
	if err != nil {
		return fmt.Errorf("erro ao buscar conta: %w", err)
	}

	// 2. credita na Blnk (idempotente via reference)
	descricao := "Depósito confirmado, id: " + depositoID
	if err := s.blnkClient.Creditar(conta.BalanceID, depositoID, valor, moeda, descricao); err != nil {
		return fmt.Errorf("erro ao creditar: %w", err)
	}

	return nil
}