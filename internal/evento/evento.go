package evento

type EventoEnvelope struct {
	Data DepositoConfirmado `json:"data"`
	ID   string             `json:"id"`
}

type DepositoConfirmado struct {
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