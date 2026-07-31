# payment-ledger — команды стенда и приложения.
# Код (миграции, оркестратор, сервер, консьюмер) реализуешь сам; цели — ориентир.

DC        := docker compose -f deploy/docker-compose.yml
CONNECT   := http://localhost:8083

# DSN по шардам (порты проброшены наружу; внутри сети сервисы ходят по именам).
DSN_A     ?= postgres://ledger:ledger@localhost:5438/shard_a?sslmode=disable
DSN_B     ?= postgres://ledger:ledger@localhost:5439/shard_b?sslmode=disable
DSN_ORCH  ?= postgres://ledger:ledger@localhost:5440/orchestrator?sslmode=disable

.PHONY: up up-cdc down clean logs \
        psql-a psql-b psql-orch \
        migrate run test tidy \
        connector connector-status consume

## --- стенд ---

up:            ## три Postgres: shard-a, shard-b, orchestrator
	$(DC) up -d shard-a shard-b orchestrator

up-cdc:        ## + Redpanda + Debezium + Console
	$(DC) --profile cdc up -d

down:          ## погасить всё (данные в volume сохраняются)
	$(DC) --profile cdc down

clean:         ## погасить и снести данные
	$(DC) --profile cdc down -v

logs:
	$(DC) logs -f

psql-a:
	docker exec -it pl-shard-a psql -U ledger -d shard_a
psql-b:
	docker exec -it pl-shard-b psql -U ledger -d shard_b
psql-orch:
	docker exec -it pl-orchestrator psql -U ledger -d orchestrator

## --- приложение (реализуешь сам) ---

migrate:       ## накатить миграции на все три БД (goose/migrate — на выбор)
	@echo "TODO: подключи миграции. Каталоги-ориентир:"
	@echo "  goose -dir ./migrations/shard        postgres \"$(DSN_A)\"    up"
	@echo "  goose -dir ./migrations/shard        postgres \"$(DSN_B)\"    up"
	@echo "  goose -dir ./migrations/orchestrator postgres \"$(DSN_ORCH)\" up"

run:           ## запустить API + оркестратор (+ воркер саги)
	go run ./cmd/api

consumer:      ## запустить exactly-once консьюмер outbox-событий
	go run ./cmd/consumer

test:
	go test ./... -race -count=1

tidy:
	go mod tidy && go vet ./...

## --- Debezium (профиль cdc) ---

connector:         ## зарегистрировать outbox-коннектор (читает orchestrator.outbox)
	curl -sS -X POST $(CONNECT)/connectors \
		-H 'Content-Type: application/json' \
		-d @deploy/debezium-connector.json | jq .

connector-status:
	curl -sS $(CONNECT)/connectors/pl-outbox/status | jq .

consume:           ## читать лайфсайкл-события переводов
	docker exec -it pl-redpanda rpk topic consume outbox.event.Transfer
