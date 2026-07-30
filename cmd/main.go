package main

import (
	"context" // novo
	"fmt"
	"os" // novo

	"github.com/Gabox99/pam/internal/db" // novo
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool" // novo

	// pro pgx.ErrNoRows

	pamv1 "github.com/Gabox99/pam/gen/pam/v1" // o pacote pamv1 gerado
	"google.golang.org/grpc/reflection"

	grpctransport "github.com/Gabox99/pam/internal/transport/grpc" // teu pacote, com alias
	"google.golang.org/grpc"        
	
	httptransport "github.com/Gabox99/pam/internal/transport/http"

	"net"

	"github.com/Gabox99/pam/internal/blnk"
	"github.com/Gabox99/pam/internal/conta"
	"github.com/Gabox99/pam/internal/deposito"
)

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
		panic(err)
	}

	// wiring das dependências
	blnkClient := blnk.New("http://blnk:5001", os.Getenv("BLNK_LEDGER_ID"))
	contaService := conta.New(queries, blnkClient)
	depositoService := deposito.New(queries, blnkClient)

	// transporte HTTP
	r := gin.Default()
	httpHandlers := httptransport.New(contaService, depositoService)
	httpHandlers.RegistrarRotas(r)

	// transporte gRPC (em paralelo)
	go func() {
		lis, err := net.Listen("tcp", ":50051")
		if err != nil {
			panic(err)
		}
		grpcServer := grpc.NewServer()
		pamv1.RegisterPamServiceServer(grpcServer, grpctransport.New(contaService))
		reflection.Register(grpcServer)
		fmt.Println("servidor gRPC ouvindo na porta 50051")
		if err := grpcServer.Serve(lis); err != nil {
			panic(err)
		}
	}()

	r.Run(":8080")
}
