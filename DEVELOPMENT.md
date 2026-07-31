# DEVELOPMENT — пошаговый гайд по сборке

Рабочий план для реализации боевого сценария из [`README.md`](./README.md).
Разбит на фазы: каждая заканчивается **проверяемым** результатом (тест или
`curl`), чтобы не строить всё сразу и не отлаживать вслепую.

Здесь — что и в каком порядке делать, с сигнатурами, SQL-схемами и критериями
готовности. **Логику движения денег и переходы саги пишешь сам** — это суть
практики; гайд даёт каркас и границы, а не готовое решение.

Легенда: 🎯 цель · 📁 файлы · 🔨 задачи · ✅ definition of done фазы.

---

## Фаза 0. Починить то, что уже сломано

Три бага в текущем каркасе. Локальные, но без них не соберётся/не поедет.

**0.1 — `Money`: развести «сумму» и «величину проводки».**
`Posting.Amount` знаковый (списание < 0), а `NewMoney` запрещает ≤ 0 —
противоречие. В [ledger/money.go](./ledger/money.go):
- `Money` остаётся сырым знаковым `int64` без валидации в конструкторе;
- добавь `func ParseAmount(minor int64) (Money, error)` — валидатор **входной
  суммы запроса** (строго > 0);
- проводки создавай из сырого `Money` (может быть отрицательным).

**0.2 — баланс на пустом счёте.**
[store/postgres.go](./store/postgres.go): `SELECT SUM(amount)` без строк вернёт
`NULL` → `Scan` в `int64` упадёт. Замени на `COALESCE(SUM(amount), 0)` в
`Balance` и везде, где суммируешь проводки.

**0.3 — имена колонок outbox под Debezium.**
Контракт Outbox Event Router — `aggregatetype`, `aggregateid` (без подчёркиваний).
Приведи схему к контракту (это делаем в фазе 1 при разбиении миграций).

✅ **DoD:** `go build ./...` чисто; тест «создать счёт → прочитать баланс = 0»
зелёный.

---

## Фаза 1. Схема и миграции (split)

🎯 Разнести единый `001_init.sql` на два набора: shard и orchestrator.
📁 `migrations/shard/`, `migrations/orchestrator/`, удалить `migrations/001_init.sql`.

**1.1 — `migrations/shard/` (катится на shard-a и shard-b одинаково):**

```sql
CREATE TABLE accounts (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  currency   CHAR(3) NOT NULL,
  kind       TEXT NOT NULL DEFAULT 'user',   -- 'user' | 'suspense'
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- проводки: append-only, знаковые, идемпотентны по шагу саги
CREATE TABLE postings (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  saga_id    UUID NOT NULL,
  step       TEXT NOT NULL,                  -- 'reserve'|'commit'|'confirm'|'release'
  account_id UUID NOT NULL REFERENCES accounts(id),
  amount     BIGINT NOT NULL,                -- знаковое, в минорных единицах
  currency   CHAR(3) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- идемпотентность шага: повтор reserve той же саги не задвоит проводки
CREATE UNIQUE INDEX postings_saga_step_acct ON postings (saga_id, step, account_id);
CREATE INDEX postings_account ON postings (account_id);

-- технические suspense-счета создаём сидом (по одному на валюту, что используешь)
INSERT INTO accounts (id, currency, kind) VALUES
  ('00000000-0000-0000-0000-00000000a115', 'USD', 'suspense'); -- interbank/rail
```

> Один suspense-счёт на валюту достаточно для учебного стенда. В shard-a он играет
> `interbank_suspense_A` и `rail_suspense_A`; в shard-b — `interbank_suspense_B`.
> Хочешь чище — заведи отдельные suspense под interbank и под rail.

**1.2 — `migrations/orchestrator/`:**

```sql
CREATE TABLE saga (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  idempotency_key TEXT NOT NULL,
  from_account    UUID NOT NULL,
  from_shard      TEXT NOT NULL,             -- 'a'|'b'
  to_account      UUID,                      -- NULL если перевод на rail
  to_shard        TEXT,
  rail_ref        TEXT,                       -- NULL если внутренний
  external_ref    TEXT,                       -- идемпотентность на rail
  amount          BIGINT NOT NULL,
  currency        CHAR(3) NOT NULL,
  state           TEXT NOT NULL,              -- CREATED|RESERVED|APPLIED|SENT|POSTED|FAILED|COMPENSATING|REVERSED
  request_hash    TEXT NOT NULL,              -- для 409 при повторе ключа с др. параметрами
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX saga_idem ON saga (idempotency_key);
CREATE INDEX saga_pending ON saga (state, updated_at);  -- для воркера

CREATE TABLE saga_step (
  saga_id   UUID NOT NULL REFERENCES saga(id),
  step      TEXT NOT NULL,
  status    TEXT NOT NULL,                    -- pending|done|failed
  attempt   INT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (saga_id, step)
);

-- outbox: имена колонок строго под Debezium Outbox Event Router
CREATE TABLE outbox (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  aggregatetype TEXT NOT NULL,                -- 'Transfer'
  aggregateid   UUID NOT NULL,
  type          TEXT NOT NULL,                -- 'TransferPosted'|'TransferReversed'
  payload       JSONB NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at  TIMESTAMPTZ                   -- для app-relay fallback; Debezium игнорирует
);

CREATE TABLE processed_events (               -- дедуп консьюмера (exactly-once)
  event_id     UUID PRIMARY KEY,
  processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE recon_exception (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  kind        TEXT NOT NULL,                  -- 'stuck'|'rail_missing'|'ours_missing'
  saga_id     UUID,
  external_ref TEXT,
  status      TEXT NOT NULL DEFAULT 'open',   -- open|resolved
  detail      JSONB,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

**1.3 — подключить миграции** (goose/migrate). Обнови `make migrate`, чтобы
катал `shard/` на обе shard-БД и `orchestrator/` на orchestrator.

✅ **DoD:** `make up && make migrate` проходит без ошибок; в трёх БД видны
таблицы (`make psql-a` / `psql-orch`, `\dt`).

---

## Фаза 2. Shard-стор и проводки

🎯 Один шард умеет создавать счета, читать баланс и делать шаги саги как
идемпотентные локальные транзакции.
📁 `shard/` (переезд из `store/`), `store/memory.go` оставить как in-memory shard.

**2.1 — интерфейс `ShardStore`:**

```go
type ShardStore interface {
    CreateAccount(ctx context.Context, currency string) (ledger.Account, error)
    GetAccount(ctx context.Context, id string) (ledger.Account, error)
    Balance(ctx context.Context, accountID string) (ledger.Money, error)

    // Шаги саги — каждый ОДНА локальная tx, идемпотентна по (sagaID, step).
    Reserve(ctx context.Context, sagaID, from string, amt ledger.Money) error // user→suspense
    Confirm(ctx context.Context, sagaID string) error                          // финализация
    Release(ctx context.Context, sagaID string) error                          // компенсация
    Commit(ctx context.Context, sagaID, to string, amt ledger.Money) error     // suspense→user

    Ping(ctx context.Context) error
}
```

**2.2 — как реализовать шаг идемпотентно и атомарно (это ядро задачи):**
- открой `tx`;
- проверку баланса делай под блокировкой: `SELECT ... FOR UPDATE` по строке
  счёта **или** полагайся на то, что баланс = сумма проводок + вставка с
  проверкой;
- вставь **две** проводки (user и suspense) с `(saga_id, step)`;
- на конфликте по `postings_saga_step_acct` (повторный шаг) — не ошибка, а
  «уже сделано»: откати вставку, верни успех (идемпотентность). Отличай этот
  случай через `errors.Is(pgErr, unique_violation)` / `ON CONFLICT DO NOTHING` +
  проверка факта.
- **инвариант шага:** сумма двух проводок = 0; шард остаётся сбалансированным.

**2.3 — тесты фазы (memory-стор годится):**
- reserve уводит деньги user→suspense, сумма проводок счёта-пары = 0;
- нехватка средств → ошибка, проводок нет;
- повтор reserve той же саги → проводки не задвоились (идемпотентность);
- конкурентные reserve с одного счёта под `-race` не уводят в минус.

✅ **DoD:** тесты 2.3 зелёные под `-race` на memory-сторе; на Postgres reserve
работает (проверь `make psql-a`, посмотри проводки).

---

## Фаза 3. Оркестратор и сага A→B (happy-path + компенсация)

🎯 Перевод между двумя шардами доходит до `POSTED`; сбой на `commit` откатывает
до `REVERSED`; состояние durable.
📁 `saga/` (оркестратор, машина состояний, воркер), `store` оркестратора.

**3.1 — `OrchestratorStore`:**

```go
type OrchestratorStore interface {
    CreateSaga(ctx, req ledger.TransferRequest) (Saga, bool, error) // bool=created; unique idem_key
    GetSaga(ctx, id string) (Saga, error)
    SetState(ctx, id string, from, to State) error                  // CAS-переход по прежнему состоянию
    LoadPending(ctx, olderThan time.Duration, limit int) ([]Saga, error) // FOR UPDATE SKIP LOCKED
    // терминальный переход + outbox в ОДНОЙ tx:
    Finalize(ctx, id string, to State, ev outbox.Event) error
    Ping(ctx) error
}
```

**3.2 — оркестратор (`Run(ctx, saga)`):** реализуй машину из README:
`reserve(from) → commit(to) → confirm(from) → POSTED`; при ошибке `commit` →
`COMPENSATING → release(from) → REVERSED`. Каждый переход состояния — через
CAS (`SetState(from,to)`), чтобы два воркера не гнали одну сагу.

**3.3 — воркер восстановления:** горутина в цикле `LoadPending` (саги в
`RESERVED`/`APPLIED`/`COMPENSATING` старше SLA) → до-Run. Останавливается по
`ctx`. Это то, что не даёт зависнуть после краша.

**3.4 — lost-ack:** перед компенсацией `release` оркестратор/стор обязан
идемпотентно проверить, не выполнился ли `commit` на самом деле (по факту
проводки `(saga_id,'commit')` на shard-b). Не откатывай вслепую по таймауту.

**3.5 — тесты фазы:**
- happy-path A→B: балансы сошлись; сумма проводок по A+B = 0; `POSTED`;
- сбой commit (инъекция ошибки в shard-b) → `REVERSED`, резерв возвращён;
- краш между шагами (прерви Run) → воркер доигрывает; нет зависших `RESERVED`;
- lost-ack: commit прошёл, «ответ потерян» → нет двойного движения.

✅ **DoD:** тесты 3.5 зелёные под `-race`; `suspense_A == -suspense_B` после
серии переводов.

---

## Фаза 4. HTTP API поверх саги

🎯 Перевод инициируется по HTTP, идемпотентность по заголовку, корректные коды.
📁 [httpapi/](./httpapi/), [cmd/api/main.go](./cmd/api/main.go).

**4.1 — обработчики** ([httpapi/handlers.go](./httpapi/handlers.go)):
- `POST /accounts {shard,currency}` → создать в нужном шарде;
- `POST /transfers`: достать `Idempotency-Key` (нет → `400`); распарсить сумму
  как строку/целое (**не float**); вызвать `CreateSaga` → запустить `Run`
  (или отдать воркеру) → `202` + тело саги; повтор с тем же ключом → `200`;
  тот же ключ с другим `request_hash` → `409`; бизнес-отказ → `422`;
- `GET /transfers/{id}` → состояние саги;
- `GET /healthz` → ping всех трёх сторов.

**4.2 — middleware:** дозаполни `RequestLog` ([middleware.go](./httpapi/middleware.go))
структурным `slog` (метод, путь, статус, латентность). `Recover` уже есть.

**4.3 — main:** три DSN из env, три пула, оркестратор + воркер саги; graceful
shutdown уже заложен — добавь остановку воркера по `ctx`.

**4.4 — проверка `curl`:** создать счета на A и B, перевод, повтор с тем же
ключом, посмотреть коды и баланс.

✅ **DoD:** `make run` + `curl`-сценарий из [VALIDATION.md](./VALIDATION.md)
проходит; коды `202/200/409/422/400` корректны.

---

## Фаза 5. Внешний rail

🎯 Перевод на rail живёт в `SENT`, таймаут не решается деньгами вслепую.
📁 `rail/` (stub), ветка саги для rail.

**5.1 — rail stub:** сервис/пакет с методом `Send(ctx, externalRef, amt)` →
`ACCEPTED|REJECTED|<timeout>`; идемпотентность по `externalRef` (повтор =
тот же ответ). Добавь управляемый режим сбоев для тестов (форсить timeout/reject).

**5.2 — ветка саги:** `reserve(from)` (user→rail_suspense) → `send` → `SENT`;
`ACCEPTED`→`confirm`→`POSTED`; `REJECTED`→`release`→`REVERSED`; `timeout`→
остаёмся `SENT`, идемпотентный ретрай; долгий `SENT` добьёт recon (фаза 7).

**5.3 — тесты:** accepted→POSTED; rejected→REVERSED; timeout→остаётся SENT и
ретрай не задваивает внешний платёж.

✅ **DoD:** три исхода rail покрыты тестом; в `SENT` перевод не завершается как
`POSTED`.

---

## Фаза 6. Outbox → Debezium → exactly-once consumer

🎯 Лайфсайкл-события надёжно доходят до Kafka; консьюмер обрабатывает
exactly-once.
📁 [outbox/](./outbox/), `cmd/consumer/`.

**6.1 — запись outbox:** в `Finalize` (фаза 3.1) строка `outbox` пишется в той
же tx, что и терминальный переход. Проверь: откат саги ⇒ события нет.

**6.2 — поднять CDC:** `make up-cdc && make connector`; `make connector-status`
= RUNNING; `make consume` показывает событие после перевода. Ключ = `aggregateid`.

**6.3 — консьюмер** (`cmd/consumer`): читает `outbox.event.Transfer`; в одной
tx: `INSERT INTO processed_events(event_id) ON CONFLICT DO NOTHING` + side-effect
(например, запись в read-model/лог нотификаций). Если конфликт — событие уже
обработано, side-effect не повторяем. **Балансы консьюмер не трогает.**

**6.4 — тест:** доставь одно событие дважды → ровно один side-effect.

✅ **DoD:** событие видно в топике; двойная доставка = один side-effect;
`make down` не теряет уже проведённые переводы (они в БД, не в памяти).

> Fallback без Kafka: app-relay ([outbox/relay.go](./outbox/relay.go)) поллит
> `outbox` через `FOR UPDATE SKIP LOCKED`, публикует, ставит `published_at`.
> Держи его рабочим для среды без Debezium, но основной путь — CDC.

---

## Фаза 7. Reconciliation

🎯 Фоновая сверка ловит то, что просочилось; идемпотентна к повтору.
📁 `recon/`.

**7.1 — внутренние инварианты:** сумма всех проводок по A+B = 0;
`balance(suspense_A) == -balance(suspense_B)`; нет `RESERVED`/`SENT` старше SLA
(иначе → до-Run или пометка `stuck` в `recon_exception`).

**7.2 — сверка с rail:** rail отдаёт список принятых `external_ref` за период:
- `SENT` + есть в выписке → довести до `POSTED`;
- `SENT` + нет и вышел таймаут → компенсировать → `REVERSED`;
- нет у нас + есть в выписке → `recon_exception(kind='rail_missing')` +
  дозачисляющая проводка, чтобы ledger сошёлся.

**7.3 — идемпотентность джоба:** повторный прогон не создаёт новых исправлений
(опирайся на `recon_exception.status` и на факт уже сделанных проводок).

**7.4 — тесты:** сценарии 7–8 из README; повторный прогон — без новых
исправлений.

✅ **DoD:** три типа расхождений чинятся; повторный прогон идемпотентен;
инвариант нуля держится.

---

## Фаза 8. Нагрузка и финальная проверка инвариантов

🎯 Доказать, что под конкуренцией и сбоями деньги не текут.

**8.1 — property-тест:** серия случайных переводов (внутренних и на rail) с
инъекцией сбоев (rail timeout/reject, ошибки шардов) в N горутин; в конце —
сумма всех проводок по A+B = 0, ни одной саги не в промежуточном состоянии.
Гоняется под `-race`.

**8.2 — прогон DoD** из [README.md](./README.md) целиком.

✅ **DoD:** весь чек-лист README закрыт; `go test ./... -race` зелёный.

---

## Как отдавать на ревью

После любой фазы (или всего) — отдай каталог агенту-ревьюеру по
[VALIDATION.md](./VALIDATION.md). Приоритет проверки: корректность денег >
распределённые инварианты > идемпотентность/outbox/recon > стиль.

## Частые грабли (свод)

- Две схемы в одном Postgres вместо двух БД → возвращается общий коммит, задача
  вырождается. Шарды — **разные инстансы/соединения**.
- Компенсация `release` поверх успевшего `commit` (lost-ack) → двойное движение.
  Всегда идемпотентная проверка факта, а не действие по таймауту.
- Баланс читается без блокировки, списание считается в Go → lost update под
  конкуренцией. Блокировка/ограничение на уровне БД.
- Идемпотентность на предварительном SELECT без unique-constraint → две
  конкурентные саги с одним ключом обе проходят. Опора на unique-индекс.
- `float` для денег где угодно, включая JSON, → мгновенный fail.
- outbox публикуется из кода после `commit` вместо записи в tx → потеря события
  при краше между commit и publish.
