package conta

import (
	"context" // novo
	"errors"  // stdlib, pro errors.Is e errors.As
	"fmt"

	"github.com/Gabox99/pam/internal/db" // novo

	"github.com/Gabox99/pam/internal/blnk"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn" // pro pgx.ErrNoRow
)

type Service struct {
	queries    *db.Queries
	blnkClient *blnk.Client
}

func New(queries *db.Queries, blnkClient *blnk.Client) *Service {
	return &Service{queries: queries, blnkClient: blnkClient}
}

// CriarConta cria a conta de um usuário (idempotente).
// Retorna erro apenas em falhas reais; "já existe" não é erro.
func (s *Service) CriarConta(ctx context.Context, userID string) error {
	// 1. verifica se já existe
	_, err := s.queries.BuscarContaPorUsuario(ctx, userID)
	if err == nil {
		return nil // já existe → sucesso, nada a fazer
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("erro ao buscar conta: %w", err)
	}

	// 2. cria o balance
	balanceID, err := s.blnkClient.CriarBalance("BRL")
	if err != nil {
		return fmt.Errorf("erro ao criar balance: %w", err)
	}

	// 3. grava o mapeamento
	_, err = s.queries.CriarConta(ctx, db.CriarContaParams{UserID: userID, BalanceID: balanceID})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil // corrida: já existe → sucesso
		}
		return fmt.Errorf("erro ao gravar conta: %w", err)
	}

	return nil
}

// sentinel error pra "usuário sem conta"
var ErrContaNaoEncontrada = errors.New("usuario sem conta")

// ConsultarSaldo retorna o saldo do usuário.
// Retorna ErrContaNaoEncontrada se o usuário não tem conta.
func (s *Service) ConsultarSaldo(ctx context.Context, userID string) (int64, string, error) {
	conta, err := s.queries.BuscarContaPorUsuario(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", ErrContaNaoEncontrada
	}
	if err != nil {
		return 0, "", fmt.Errorf("erro ao buscar conta: %w", err)
	}

	saldo, moeda, err := s.blnkClient.ConsultarSaldo(conta.BalanceID)
	if err != nil {
		return 0, "", fmt.Errorf("erro ao consultar saldo: %w", err)
	}

	return saldo, moeda, nil
}
