# payment-ledger — команды стенда и приложения.

DC        := docker compose -f deploy/docker-compose.yml
CONNECT   := http://localhost:8083

# DSN одной БД (порт проброшен наружу; внутри сети сервисы ходят по имени).
DSN       ?= postgres://ledger:ledger@localhost:5432/ledger?sslmode=disable

GOOSE     ?= go run github.com/pressly/goose/v3/cmd/goose@v3.24.1

.PHONY: up up-cdc down clean logs psql \
        migrate run consumer test tidy \
        connector connector-status consume

## --- стенд ---

up:            ## Postgres `ledger`
	$(DC) up -d ledger

up-cdc:        ## + Redpanda + Debezium + Console
	$(DC) --profile cdc up -d

down:          ## погасить всё (данные в volume сохраняются)
	$(DC) --profile cdc down

clean:         ## погасить и снести данные
	$(DC) --profile cdc down -v

logs:
	$(DC) logs -f

psql:
	docker exec -it pl-ledger psql -U ledger -d ledger

## --- приложение (реализуешь сам) ---

migrate:       ## накатить миграции
	$(GOOSE) -dir ./migrations postgres "$(DSN)" up

run:           ## запустить API + ledger
	go run ./cmd/api

consumer:      ## запустить exactly-once консьюмер outbox-событий (нотификации)
	go run ./cmd/consumer

test:
	go test ./... -race -count=1

tidy:
	go mod tidy && go vet ./...

## --- Debezium (профиль cdc) ---

connector:         ## зарегистрировать outbox-коннектор (читает ledger.outbox)
	curl -sS -X POST $(CONNECT)/connectors \
		-H 'Content-Type: application/json' \
		-d @deploy/debezium-connector.json | jq .

connector-status:
	curl -sS $(CONNECT)/connectors/pl-outbox/status | jq .

consume:           ## читать лайфсайкл-события переводов
	docker exec -it pl-redpanda rpk topic consume outbox.event.Transfer
