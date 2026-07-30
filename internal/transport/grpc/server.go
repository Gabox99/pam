package grpc

import (
	"context"

	pamv1 "github.com/Gabox99/pam/gen/pam/v1"
	"github.com/Gabox99/pam/internal/conta"

	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	pamv1.UnimplementedPamServiceServer
	contaService *conta.Service
}

func New(contaService *conta.Service) *Server {
	return &Server{contaService: contaService}
}

func (s *Server) ConsultarSaldo(ctx context.Context, req *pamv1.ConsultarSaldoRequest) (*pamv1.ConsultarSaldoResponse, error) {
	saldo, moeda, err := s.contaService.ConsultarSaldo(ctx, req.UserId)
	if errors.Is(err, conta.ErrContaNaoEncontrada) {
		return nil, status.Errorf(codes.NotFound, "usuario sem conta: %s", req.UserId)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "erro ao consultar saldo: %v", err)
	}

	return &pamv1.ConsultarSaldoResponse{
		Saldo: saldo,
		Moeda: moeda,
	}, nil
}