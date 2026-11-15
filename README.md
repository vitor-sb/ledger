# ledger

Ledger de despesas agendadas com previsão de pagamentos futuros (Go + PostgreSQL).

```sh
cp .env.example .env   # defina POSTGRES_PASSWORD (o .env não é versionado)
make db     # sobe o Postgres (porta 5434)
make run    # API em :8080 (aplica as migrations ao subir)
make test   # unitários + integração, com -race
```

## API

| Método e rota | O que faz |
|---|---|
| `POST /expenses` | Cadastra uma despesa (`title`, `amount_cents`, `first_due_date`, `recurrence`, `installment_count`) |
| `GET /expenses?limit=&after=` | Lista despesas, paginado por cursor |
| `POST /expenses/{id}/cancellation` | Cancela a despesa a partir de `effective_date` (opcional, padrão hoje) |
| `POST /expenses/{id}/skipped-installments` | Pula uma parcela (`installment_number`) sem mexer no resto da série |
| `GET /upcoming-payments?from=&months=&limit=&after=` | Pagamentos futuros em ordem, paginado por cursor |
| `GET /monthly-forecast?from=&months=` | Total e quantidade de pagamentos por mês (máx. 24 meses) |
| `GET /openapi.yaml` | Especificação OpenAPI 3 da API (validada em teste com `kin-openapi`) |

Paginação: a resposta traz `next_cursor` quando há mais itens; envie-o como `after` na chamada seguinte.
O cursor é opaco (não monte nem interprete). `limit`: 1 a 100, padrão 20.
Erros: 400 requisição malformada, 404 não encontrado, 409 conflito (já cancelada, parcela já pulada),
422 regra de negócio violada.

```sh
curl -XPOST localhost:8080/expenses -d '{"title":"Aluguel","amount_cents":150000,"first_due_date":"2026-11-05","recurrence":"MENSAL"}'
curl -XPOST localhost:8080/expenses/1/skipped-installments -d '{"installment_number":2}'
curl 'localhost:8080/upcoming-payments?months=6&limit=5'
```

## Estrutura

```
cmd/ledger                      composition root: liga as peças, shutdown gracioso
internal/ledger                 regras de negócio, sem HTTP/JSON/SQL
  expense.go                    Expense, ScheduledPayment, expansão das parcelas
  expense_registry.go           ExpenseRegistry: cadastrar, cancelar, pular parcela, listar
  payment_forecaster.go         PaymentForecaster: pagamentos futuros e previsão mensal
internal/postgres               ExpenseRepository (pgx) + migrations embutidas
internal/httpapi                handlers, DTOs, cursores, middlewares
```

- **Dependências apontam para dentro**: `ledger` não importa nada do projeto. Cada serviço declara a
  interface de que precisa (`ExpenseStore`, `ExpenseSource`), e `postgres.ExpenseRepository` as satisfaz.
- **Responsabilidades separadas**: `ExpenseRegistry` altera estado; `PaymentForecaster` só lê e calcula.
- **Migrations**: `goose` (SQL embutido em `internal/postgres/migrations`), aplicadas ao subir, com lock de sessão para várias instâncias.
- **Ledger append-only**: cancelar e pular parcela *acrescentam* registros (`expense_cancellations`,
  `skipped_installments`); nada é apagado nem editado.
- Dinheiro em centavos (`int64`); fim de mês sem drift (31/jan -> 28/fev -> 31/mar).
- Paginação keyset: `WHERE id > $cursor ORDER BY id LIMIT n`, custo constante em qualquer página.

## Limites conhecidos

- A previsão é calculada em memória a cada chamada (todas as despesas, janela de até 24 meses). Serve para
  milhares de despesas; depois disso, mover a expansão para SQL (`generate_series`) ou fazer cache.
- Sem autenticação nem multiusuário. Sem observabilidade além de logs.
