# ledger

Ledger de despesas agendadas com previsão de pagamentos futuros (Go + PostgreSQL).

```sh
cp .env.example .env   # defina POSTGRES_PASSWORD (o .env não é versionado)
make db     # sobe o Postgres (porta 5434)
make run    # API em :8080 (aplica as migrations ao subir)
make test   # unitários + integração, com -race
make bench  # benchmarks (não precisam de banco)
make fuzz   # fuzzing das regras de data e da expansão (FUZZTIME=30s)
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
  expansion.go                  expansão concorrente das despesas em pagamentos (errgroup + merge)
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

## Desempenho

A previsão carrega todas as despesas e expande cada uma em pagamentos. Os números abaixo são de
`make bench` (24 meses, mix de recorrências com 1/8 de despesas diárias), numa máquina de 4 vCPUs,
uma rodada só: servem para comparar as etapas, não como garantia.

| Despesas | Inicial | Pré-alocação | + concorrência (4 workers) |
|---|---|---|---|
| 1.000 | 135 ms | 81 ms | 37 ms |
| 10.000 | 1,46 s | 0,96 s | 0,48 s |
| 50.000 | 9,2 s | 5,1 s | 2,4 s |

O que mudou e por quê (o profile guiou cada passo, ver `make profile`):

- **Pré-alocação**: o `append` crescendo o slice e o `Format` do mês a cada pagamento dominavam o custo.
  Dimensionar o slice de antemão e formatar o mês só quando ele muda tirou 1,2 milhão de alocações em
  10 mil despesas (31 sobraram) e baixou a memória de 626 MB para 75 MB.
- **Concorrência** (`expansion.go`): as despesas são independentes, então cada goroutine expande e ordena
  um bloco, e os blocos ordenados são mesclados. `PaymentKey` é uma ordem total sem empates, então o
  resultado não depende do número de workers (o teste compara com a versão sequencial). Abaixo de
  128 despesas por worker nada disso é usado. Custo: ~3x mais memória, porque o merge copia.
- **Fuzzing**: `addMonthsClamped` e a expansão têm invariantes (dentro do período, ordenado, sem
  cancelado/pulado, dentro da capacidade estimada) verificadas com entradas geradas.
- **pprof** (opcional): `PPROF_ADDR=127.0.0.1:6060 make run` abre os profiles num listener separado
  da API, desligado por padrão. `go tool pprof http://127.0.0.1:6060/debug/pprof/profile?seconds=10`.

## Limites conhecidos

- A previsão ainda é calculada em memória a cada chamada (todas as despesas, janela de até 24 meses).
  Pelos benchmarks acima, até ~1.000 despesas responde em dezenas de milissegundos; com 10 mil já passa
  de meio segundo. Daí em diante, mover a expansão para SQL (`generate_series`) ou fazer cache.
- `MonthlyForecast` só precisa dos totais por mês e hoje também ordena todos os pagamentos; agregar sem
  ordenar seria o próximo ganho barato (não feito).
- A expansão de uma despesa diária percorre as parcelas desde a primeira data de vencimento.
- Sem autenticação nem multiusuário. Sem observabilidade além de logs e do pprof opcional.
