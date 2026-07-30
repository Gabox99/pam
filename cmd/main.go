package main

import (
	"bytes"
	"context" // novo
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os" // novo
	"strings"

	"github.com/Gabox99/pam/internal/db" // novo
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool" // novo

	"errors" // stdlib, pro errors.Is e errors.As

	"github.com/jackc/pgx/v5"        // pro pgx.ErrNoRows
	"github.com/jackc/pgx/v5/pgconn" // pro pgconn.PgError

	pamv1 "github.com/Gabox99/pam/gen/pam/v1" // o pacote pamv1 gerado
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	"net"

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

type CriarBalanceReq struct {
	LedgerID string `json:"ledger_id"`
	Currency string `json:"currency"`
}

type BalanceResp struct {
	BalanceID string `json:"balance_id"`
}

type TransacaoBlnk struct {
	Amount         int64  `json:"amount"`
	Precision      int    `json:"precision"`
	Reference      string `json:"reference"`
	Description    string `json:"description"`
	Currency       string `json:"currency"`
	Source         string `json:"source"`
	Destination    string `json:"destination"`
	AllowOverdraft bool   `json:"allow_overdraft"`
	SkipQueue      bool   `json:"skip_queue"`
}

type SaldoBlnkResp struct {
	Balance  int64  `json:"balance"`
	Currency string `json:"currency"`
}

type pamServer struct {
	pamv1.UnimplementedPamServiceServer
	queries *db.Queries
}

func consultarSaldoNaBlnk(balanceID string) (int64, string, error) {
	resp, err := http.Get("http://blnk:5001/balances/" + balanceID)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		corpoResp, _ := io.ReadAll(resp.Body)
		return 0, "", fmt.Errorf("blnk retornou status %d ao consultar saldo: %s", resp.StatusCode, string(corpoResp))
	}

	var saldoResp SaldoBlnkResp
	if err := json.NewDecoder(resp.Body).Decode(&saldoResp); err != nil {
		return 0, "", err
	}
	return saldoResp.Balance, saldoResp.Currency, nil
}

func (s *pamServer) ConsultarSaldo(ctx context.Context, req *pamv1.ConsultarSaldoRequest) (*pamv1.ConsultarSaldoResponse, error) {
	// 1. busca a conta do usuário
	conta, err := s.queries.BuscarContaPorUsuario(ctx, req.UserId)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, status.Errorf(codes.NotFound, "usuario sem conta: %s", req.UserId)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "erro ao buscar conta: %v", err)
	}

	// 2. consulta o saldo na Blnk
	saldo, moeda, err := consultarSaldoNaBlnk(conta.BalanceID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "erro ao consultar saldo: %v", err)
	}

	// 3. retorna a resposta
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

func creditarNaBlnk(deposito DepositoConfirmado, balanceID string) error {
	transacao := TransacaoBlnk{
		Amount:         deposito.Valor,
		Precision:      1,
		Reference:      deposito.DepositoID,
		Description:    "Depósito confirmado, id: " + deposito.DepositoID,
		Currency:       deposito.Moeda,
		Source:         "@Mundo",
		Destination:    balanceID,
		AllowOverdraft: true,
		SkipQueue:      true,
	}
	corpo, err := json.Marshal(transacao)
	if err != nil {
		return err
	}

	resp, err := http.Post("http://blnk:5001/transactions", "application/json", bytes.NewBuffer(corpo))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil // sucesso
	}

	corpoResp, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(corpoResp), "has already been used") {
		fmt.Println("deposito duplicado (já processado): ", deposito.DepositoID)
		return nil // não é um erro, apenas ignoramos depósitos duplicados
	}

	return fmt.Errorf("blnk retornou status: %d: %s ", resp.StatusCode, string(corpoResp))
}

func criarBalanceNaBlnk() (string, error) {
	corpoReq := CriarBalanceReq{
		LedgerID: os.Getenv("BLNK_LEDGER_ID"),
		Currency: "BRL",
	}
	corpo, err := json.Marshal(corpoReq)
	if err != nil {
		return "", err
	}

	resp, err := http.Post("http://blnk:5001/balances", "application/json", bytes.NewBuffer(corpo))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		corpoResp, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("blnk retornou status %d ao criar balance: %s", resp.StatusCode, string(corpoResp))
	}

	var balanceResp BalanceResp
	if err := json.NewDecoder(resp.Body).Decode(&balanceResp); err != nil {
		return "", err
	}
	return balanceResp.BalanceID, nil
}

func main() {

	queries, err := conectarBanco()
	if err != nil {
		panic(err) // se não conecta no banco, não faz sentido continuar
	}

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

		deposito := evento.Data
		fmt.Printf("Deposito recebido: id=%s, user=%s, valor=%d, moeda=%s\n",
			deposito.DepositoID, deposito.UserID, deposito.Valor, deposito.Moeda)

		// 1. busca a conta do usuário
		conta, err := queries.BuscarContaPorUsuario(c.Request.Context(), deposito.UserID)
		if errors.Is(err, pgx.ErrNoRows) {
			// usuário sem conta: rejeita (pragmático — loga e não faz loop)
			fmt.Println("REJEITADO: deposito para usuario sem conta:", deposito.UserID)
			c.JSON(200, gin.H{"status": "SUCCESS"}) // SUCCESS pro Dapr parar; erro registrado no log
			return
		}
		if err != nil {
			// erro de verdade na busca
			fmt.Println("erro ao buscar conta:", err)
			c.JSON(500, gin.H{"status": "RETRY"})
			return
		}

		// 2. credita no balance encontrado
		if err := creditarNaBlnk(deposito, conta.BalanceID); err != nil {
			fmt.Println("erro ao creditar:", err)
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

		userID := evento.Data.UserID
		fmt.Println("Conta criada recebida para user:", userID)

		// 1. verifica se a conta já existe (idempotência)
		_, err := queries.BuscarContaPorUsuario(c.Request.Context(), userID)
		if err == nil {
			// achou: conta já existe, nada a fazer
			fmt.Println("conta já existe, ignorando:", userID)
			c.JSON(200, gin.H{"status": "SUCCESS"})
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			// erro de verdade na busca (não é "não achou")
			fmt.Println("erro ao buscar conta:", err)
			c.JSON(500, gin.H{"erro": "falha ao buscar conta"})
			return
		}
		// se chegou aqui, err == pgx.ErrNoRows → usuário novo, segue

		// 2. cria o balance na Blnk
		balanceID, err := criarBalanceNaBlnk()
		if err != nil {
			fmt.Println("erro ao criar balance:", err)
			c.JSON(500, gin.H{"erro": "falha ao criar balance"})
			return
		}

		// 3. grava o mapeamento no banco
		_, err = queries.CriarConta(c.Request.Context(), db.CriarContaParams{
			UserID:    userID,
			BalanceID: balanceID,
		})
		if err != nil {
			// rede de segurança: corrida de concorrência (23505)
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				fmt.Println("conta duplicada (corrida):", userID)
				c.JSON(200, gin.H{"status": "SUCCESS"})
				return
			}
			fmt.Println("erro ao gravar conta:", err)
			c.JSON(500, gin.H{"erro": "falha ao gravar conta"})
			return
		}

		fmt.Printf("Conta criada: user=%s, balance=%s\n", userID, balanceID)
		c.JSON(200, gin.H{"status": "SUCCESS"})
	})

	// --- servidor gRPC ---
	go func() {
		lis, err := net.Listen("tcp", ":50051")
		if err != nil {
			panic(err)
		}

		grpcServer := grpc.NewServer()
		pamv1.RegisterPamServiceServer(grpcServer, &pamServer{queries: queries})
		reflection.Register(grpcServer) // <- nova linha

		fmt.Println("servidor gRPC ouvindo na porta 50051")
		if err := grpcServer.Serve(lis); err != nil {
			panic(err)
		}

	}()

	// --- servidor HTTP (Gin) — bloqueia, mantém o programa vivo ---
	r.Run(":8080")

}
