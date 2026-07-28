CREATE TABLE contas (
    id          SERIAL PRIMARY KEY,
    user_id     TEXT NOT NULL UNIQUE,
    balance_id  TEXT NOT NULL,
    criado_em   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);