-- name: CriarConta :one
INSERT INTO contas (user_id, balance_id)
VALUES ($1, $2)
RETURNING *;

-- name: BuscarContaPorUsuario :one
SELECT * FROM contas
WHERE user_id = $1;