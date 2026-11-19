# Local settings come from .env (git-ignored): cp .env.example .env
-include .env
ifndef POSTGRES_PASSWORD
$(error POSTGRES_PASSWORD is not set: copy .env.example to .env and edit it)
endif

# Inside the devcontainer the Postgres published on the host is reached via host.docker.internal.
DB_HOST ?= host.docker.internal
export DATABASE_URL      ?= postgres://ledger:$(POSTGRES_PASSWORD)@$(DB_HOST):5434/ledger?sslmode=disable
export TEST_DATABASE_URL ?= $(DATABASE_URL)

.PHONY: db run test lint bench fuzz profile
db:   ; docker compose up -d --wait db
run:  ; go run ./cmd/ledger
test: ; go test -race -count=1 ./...
lint: ; gofmt -l . && go vet ./...

# Benchmarks need no database. Compare two runs with benchstat.
bench: ; go test ./internal/ledger -run '^$$' -bench . -benchmem -count=5

FUZZTIME ?= 30s
fuzz:
	go test ./internal/ledger -run '^$$' -fuzz FuzzAddMonthsClamped -fuzztime $(FUZZTIME)
	go test ./internal/ledger -run '^$$' -fuzz FuzzScheduledPaymentsIn -fuzztime $(FUZZTIME)

# Writes cpu.prof and mem.prof; open with: go tool pprof -http=:9090 cpu.prof
profile:
	go test ./internal/ledger -run '^$$' -bench 'MonthlyForecast/expenses=10000' -benchtime=5s -cpuprofile cpu.prof -memprofile mem.prof
