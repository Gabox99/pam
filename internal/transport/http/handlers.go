package http

import (
	"errors"
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/Gabox99/pam/internal/conta"
	"github.com/Gabox99/pam/internal/deposito"
	"github.com/Gabox99/pam/internal/evento"
)

type Handlers struct {
	contaService    *conta.Service
	depositoService *deposito.Service
}

func New(contaService *conta.Service, depositoService *deposito.Service) *Handlers {
	return &Handlers{
		contaService:    contaService,
		depositoService: depositoService,
	}
}

// RegistrarRotas registra as rotas HTTP no engine Gin
func (h *Handlers) RegistrarRotas(r *gin.Engine) {
	r.GET("/dapr/subscribe", h.subscribe)
	r.POST("/conta-criada", h.contaCriada)
	r.POST("/deposito-confirmado", h.depositoConfirmado)
}

func (h *Handlers) subscribe(c *gin.Context) {
	c.JSON(200, []map[string]string{
		{
			"pubsubname": "pubsub",
			"topic":      "pam.deposito.confirmado",
			"route":      "/deposito-confirmado",
		},
		{
			"pubsubname": "pubsub",
			"topic":      "pam.conta.criada",
			"route":      "/conta-criada",
		},
	})
}

func (h *Handlers) contaCriada(c *gin.Context) {
	var ev evento.EnvelopeContaCriada
	if err := c.ShouldBindJSON(&ev); err != nil {
		c.JSON(400, gin.H{"error": "json invalido"})
		return
	}

	if err := h.contaService.CriarConta(c.Request.Context(), ev.Data.UserID); err != nil {
		fmt.Println("erro ao criar conta:", err)
		c.JSON(500, gin.H{"status": "RETRY"})
		return
	}

	c.JSON(200, gin.H{"status": "SUCCESS"})
}

func (h *Handlers) depositoConfirmado(c *gin.Context) {
	var ev evento.EventoEnvelope
	if err := c.ShouldBindJSON(&ev); err != nil {
		c.JSON(400, gin.H{"error": "json invalido"})
		return
	}

	d := ev.Data
	err := h.depositoService.ProcessarDeposito(c.Request.Context(), d.DepositoID, d.UserID, d.Valor, d.Moeda)

	if errors.Is(err, deposito.ErrContaNaoEncontrada) {
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
}