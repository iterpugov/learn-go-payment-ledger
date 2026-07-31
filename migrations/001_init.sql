-- +goose Up
-- Одна БД, одна транзакция: перевод пишет проводки, статус и outbox-событие
-- атомарно. Никаких шардов и саги — деньги двигаются одним локальным COMMIT.

CREATE TABLE accounts (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  currency   CHAR(3) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- перевод: заголовок операции + идемпотентность на уровне API
CREATE TABLE transfers (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  idempotency_key TEXT NOT NULL,
  from_account    UUID NOT NULL REFERENCES accounts(id),
  to_account      UUID NOT NULL REFERENCES accounts(id),
  amount          BIGINT NOT NULL,            -- входная сумма > 0, минорные единицы
  currency        CHAR(3) NOT NULL,
  status          TEXT NOT NULL,              -- всегда POSTED (одна tx: либо коммит, либо строки нет)
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- повтор с тем же ключом → та же операция, а не вторая
CREATE UNIQUE INDEX transfers_idem ON transfers (idempotency_key);

-- проводки: append-only, знаковые (double-entry). Баланс = SUM(amount).
CREATE TABLE postings (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  transfer_id UUID NOT NULL REFERENCES transfers(id),
  account_id  UUID NOT NULL REFERENCES accounts(id),
  amount      BIGINT NOT NULL,                -- знаковое: списание < 0, зачисление > 0
  currency    CHAR(3) NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX postings_account ON postings (account_id);
-- по одной проводке на счёт в переводе; повтор перевода не задвоит движение
CREATE UNIQUE INDEX postings_transfer_account ON postings (transfer_id, account_id);

-- outbox: имена колонок строго под Debezium Outbox Event Router
CREATE TABLE outbox (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  aggregatetype TEXT NOT NULL,                -- 'Transfer' → топик outbox.event.Transfer
  aggregateid   UUID NOT NULL,                -- id перевода (ключ партиции)
  type          TEXT NOT NULL,                -- 'TransferPosted'
  payload       JSONB NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at  TIMESTAMPTZ                   -- для app-relay fallback; Debezium игнорирует
);

-- дедуп консьюмера нотификаций (exactly-once поверх at-least-once)
CREATE TABLE processed_events (
  event_id     UUID PRIMARY KEY,
  processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS processed_events;
DROP TABLE IF EXISTS outbox;
DROP TABLE IF EXISTS postings;
DROP TABLE IF EXISTS transfers;
DROP TABLE IF EXISTS accounts;
