package main

import (
	"context" // novo
	"fmt"
	"os" // novo

	"github.com/Gabox99/pam/internal/db" // novo
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool" // novo

	"errors" // stdlib, pro errors.Is e errors.As

	// pro pgx.ErrNoRows

	pamv1 "github.com/Gabox99/pam/gen/pam/v1" // o pacote pamv1 gerado
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	"net"

	"github.com/Gabox99/pam/internal/blnk"
	"github.com/Gabox99/pam/internal/conta"
	"github.com/Gabox99/pam/internal/deposito"
	"google.golang.org/grpc"
)

type EventoEnvelope struct {
	Data DepositoConfirmado `json:"data"`
	ID   string             `json:"id"`
	// outros campos do CloudEvent se quiser
}

type DepositoConfirmado struct {
	// campos específicos do depósito confirmado

	DepositoID string `json:"deposito_id"`
	UserID     string `json:"user_id"`
	Valor      int64  `json:"valor"`
	Moeda      string `json:"moeda"`
}

type ContaCriada struct {
	UserID string `json:"user_id"`
}

type EnvelopeContaCriada struct {
	Data ContaCriada `json:"data"`
}

type pamServer struct {
	pamv1.UnimplementedPamServiceServer
	contaService *conta.Service
}

func (s *pamServer) ConsultarSaldo(ctx context.Context, req *pamv1.ConsultarSaldoRequest) (*pamv1.ConsultarSaldoResponse, error) {
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

func daprSubscribe() []map[string]string {
	return []map[string]string{{
		"pubsubname": "pubsub",
		"topic":      "pam.deposito.confirmado",
		"route":      "/deposito-confirmado",
	},
		{
			"pubsubname": "pubsub",
			"topic":      "pam.conta.criada",
			"route":      "/conta-criada",
		},
	}
}

func conectarBanco() (*db.Queries, error) {
	dsn := os.Getenv("DATABASE_URL")
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return nil, err
	}
	return db.New(pool), nil
}

func main() {

	queries, err := conectarBanco()
	if err != nil {
		panic(err) // se não conecta no banco, não faz sentido continuar
	}

	// client da Blnk
	blnkClient := blnk.New("http://blnk:5001", os.Getenv("BLNK_LEDGER_ID"))
	contaService := conta.New(queries, blnkClient)
	depositoService := deposito.New(queries, blnkClient)

	r := gin.Default()

	r.GET("/dapr/subscribe", func(c *gin.Context) {
		c.JSON(200, daprSubscribe())
	})

	r.POST("/deposito-confirmado", func(c *gin.Context) {
		var evento EventoEnvelope
		if err := c.ShouldBindJSON(&evento); err != nil {
			c.JSON(400, gin.H{"error": "json invalido"})
			return
		}

		d := evento.Data
		err := depositoService.ProcessarDeposito(c.Request.Context(), d.DepositoID, d.UserID, d.Valor, d.Moeda)

		if errors.Is(err, deposito.ErrContaNaoEncontrada) {
			// rejeição pragmática: loga e responde SUCCESS pro Dapr não retentar
			fmt.Println("REJEITADO: deposito para usuario sem conta:", d.UserID)
			c.JSON(200, gin.H{"status": "SUCCESS"})
			return
		}
		if err != nil {
			fmt.Println("erro ao processar deposito:", err)
			c.JSON(500, gin.H{"status": "RETRY"})
			return
		}

		c.JSON(200, gin.H{"status": "SUCCESS"})
	})

	r.POST("/conta-criada", func(c *gin.Context) {
		var evento EnvelopeContaCriada
		if err := c.ShouldBindJSON(&evento); err != nil {
			c.JSON(400, gin.H{"error": "json invalido"})
			return
		}

		if err := contaService.CriarConta(c.Request.Context(), evento.Data.UserID); err != nil {
			fmt.Println("erro ao criar conta:", err)
			c.JSON(500, gin.H{"status": "RETRY"})
			return
		}

		c.JSON(200, gin.H{"status": "SUCCESS"})
	})

	// --- servidor gRPC ---
	go func() {
		lis, err := net.Listen("tcp", ":50051")
		if err != nil {
			panic(err)
		}

		grpcServer := grpc.NewServer()
		pamv1.RegisterPamServiceServer(grpcServer, &pamServer{contaService: contaService})
		reflection.Register(grpcServer) // <- nova linha

		fmt.Println("servidor gRPC ouvindo na porta 50051")
		if err := grpcServer.Serve(lis); err != nil {
			panic(err)
		}

	}()

	// --- servidor HTTP (Gin) — bloqueia, mantém o programa vivo ---
	r.Run(":8080")

}
