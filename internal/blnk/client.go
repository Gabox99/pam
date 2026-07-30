package blnk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client encapsula o acesso à Blnk
type Client struct {
	baseURL  string
	ledgerID string
}

// New cria um novo client da Blnk
func New(baseURL, ledgerID string) *Client {
	return &Client{
		baseURL:  baseURL,
		ledgerID: ledgerID,
	}
}

// --- structs internas do client ---

type criarBalanceReq struct {
	LedgerID string `json:"ledger_id"`
	Currency string `json:"currency"`
}

type balanceResp struct {
	BalanceID string `json:"balance_id"`
}

type transacao struct {
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

type saldoResp struct {
	Balance  int64  `json:"balance"`
	Currency string `json:"currency"`
}

// CriarBalance cria um novo balance na Blnk e retorna o balance_id
func (c *Client) CriarBalance(currency string) (string, error) {
	corpoReq := criarBalanceReq{
		LedgerID: c.ledgerID,
		Currency: currency,
	}
	corpo, err := json.Marshal(corpoReq)
	if err != nil {
		return "", err
	}

	resp, err := http.Post(c.baseURL+"/balances", "application/json", bytes.NewBuffer(corpo))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		corpoResp, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("blnk retornou status %d ao criar balance: %s", resp.StatusCode, string(corpoResp))
	}

	var br balanceResp
	if err := json.NewDecoder(resp.Body).Decode(&br); err != nil {
		return "", err
	}
	return br.BalanceID, nil
}

// Creditar credita um valor num balance, de forma idempotente (via reference)
func (c *Client) Creditar(balanceID, reference string, valor int64, moeda, descricao string) error {
	tx := transacao{
		Amount:         valor,
		Precision:      1,
		Reference:      reference,
		Description:    descricao,
		Currency:       moeda,
		Source:         "@Mundo",
		Destination:    balanceID,
		AllowOverdraft: true,
		SkipQueue:      true,
	}
	corpo, err := json.Marshal(tx)
	if err != nil {
		return err
	}

	resp, err := http.Post(c.baseURL+"/transactions", "application/json", bytes.NewBuffer(corpo))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	corpoResp, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(corpoResp), "has already been used") {
		return nil // duplicata: já processado, tratamos como sucesso
	}

	return fmt.Errorf("blnk retornou status %d: %s", resp.StatusCode, string(corpoResp))
}

// ConsultarSaldo lê o saldo de um balance
func (c *Client) ConsultarSaldo(balanceID string) (int64, string, error) {
	resp, err := http.Get(c.baseURL + "/balances/" + balanceID)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		corpoResp, _ := io.ReadAll(resp.Body)
		return 0, "", fmt.Errorf("blnk retornou status %d ao consultar saldo: %s", resp.StatusCode, string(corpoResp))
	}

	var sr saldoResp
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return 0, "", err
	}
	return sr.Balance, sr.Currency, nil
}