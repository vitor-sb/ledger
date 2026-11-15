# Local settings come from .env (git-ignored): cp .env.example .env
-include .env
ifndef POSTGRES_PASSWORD
$(error POSTGRES_PASSWORD is not set: copy .env.example to .env and edit it)
endif

# Inside the devcontainer the Postgres published on the host is reached via host.docker.internal.
DB_HOST ?= host.docker.internal
export DATABASE_URL      ?= postgres://ledger:$(POSTGRES_PASSWORD)@$(DB_HOST):5434/ledger?sslmode=disable
export TEST_DATABASE_URL ?= $(DATABASE_URL)

.PHONY: db run test lint
db:   ; docker compose up -d --wait db
run:  ; go run ./cmd/ledger
test: ; go test -race -count=1 ./...
lint: ; gofmt -l . && go vet ./...
