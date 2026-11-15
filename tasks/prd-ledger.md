# PRD: Ledger (livro-razão de partidas dobradas)

## 1. Introdução

Hoje o projeto `ledger` é um agendador de despesas com previsão de pagamentos. Este PRD o transforma em um
**ledger de verdade**: contas, transações de partidas dobradas simples (cada transação tem exatamente 2 lançamentos,
um débito e um crédito do mesmo valor), saldos, extrato e balancete. O módulo atual de despesas agendadas continua,
como módulo separado, e passa a poder ser conciliado com o ledger (uma parcela paga vira uma transação).

Projeto de estudo/portfólio em Go. Sem usuários nem autenticação por enquanto. Moeda única (BRL), valores em centavos.

## 2. Objetivos

- Garantir por construção que o dinheiro nunca nasce nem some: toda transação fecha em zero e o balancete do ledger fecha em zero.
- Ledger **append-only**: nada é editado nem apagado; erros se corrigem com estorno.
- Operações de escrita seguras para retry (idempotência).
- Reaproveitar o módulo de previsão existente, sem reescrevê-lo.
- Código simples de explicar: poucas camadas, stdlib sempre que possível.

## 3. Glossário (para quem vem do Java)

- **Conta**: um "balde" de dinheiro. Ex.: `Conta Corrente`, `Despesas:Moradia`, `Receitas:Salário`.
- **Lançamento (entry)**: uma linha de débito ou crédito em uma conta.
- **Transação**: grupo de 2 lançamentos de mesmo valor, um débito e um crédito. É a unidade atômica.
- **Saldo**: soma dos lançamentos da conta. Não existe campo "saldo" editável.
- **Estorno**: nova transação com os lados trocados que anula a original.
- **Balancete**: soma de todos os saldos; tem que dar zero.

## 4. User Stories

### US-001: Tabela e domínio de contas
**Description:** Como desenvolvedor, preciso persistir contas para poder lançar transações nelas.

**Acceptance Criteria:**
- [ ] Migration cria a tabela de contas (id, nome único, tipo, status aberta/fechada, `created_at`) e `account_balances` (account_id PK, `balance_cents`, `updated_at`), com a linha de saldo (0) criada junto com a conta
- [ ] Tipo da conta: `asset`, `liability`, `equity`, `income` ou `expense`, validado no domínio e por `CHECK` no banco
- [ ] `NewAccount` valida nome (1 a 50 caracteres) e tipo, com teste unitário
- [ ] `make test` e `make lint` passam

### US-002: Criar e listar contas (API)
**Description:** Como usuário da API, quero criar e listar contas.

**Acceptance Criteria:**
- [ ] `POST /accounts` cria conta e retorna 201; nome repetido retorna 409; entrada inválida retorna 422
- [ ] `GET /accounts?limit=&after=` lista paginado por cursor, no mesmo padrão de `/expenses`
- [ ] `openapi.yaml` atualizado e validado pelo teste existente
- [ ] Teste de integração com Postgres cobrindo criar, duplicar e listar
- [ ] `make test` e `make lint` passam

### US-003: Fechar conta
**Description:** Como usuário da API, quero fechar uma conta que não uso mais.

**Acceptance Criteria:**
- [ ] `POST /accounts/{id}/closure` fecha a conta (registro acrescentado, sem `UPDATE`/`DELETE`)
- [ ] Fechar conta já fechada retorna 409; conta inexistente retorna 404
- [ ] Conta com saldo diferente de zero não fecha (422); a checagem usa o saldo materializado, lido com lock na mesma transação do fechamento
- [ ] Transação em conta fechada é rejeitada (coberto em US-017)
- [ ] `make test` e `make lint` passam

### US-004: Domínio da transação
**Description:** Como desenvolvedor, preciso do modelo de transação com 2 lançamentos que sempre fecham em zero, sem depender do banco.

**Acceptance Criteria:**
- [ ] `NewTransaction(debitAccountID, creditAccountID, amountCents, date, description)` valida: valor > 0, contas diferentes, descrição de 1 a 100 caracteres, data obrigatória
- [ ] A transação expõe exatamente 2 lançamentos com valor com sinal (débito positivo, crédito negativo) e soma zero
- [ ] Testes unitários de tabela cobrem cada validação e o invariante de soma zero
- [ ] `make test` e `make lint` passam

### US-017: Persistir transação e atualizar saldos
**Description:** Como desenvolvedor, preciso gravar a transação, seus lançamentos e os saldos de forma atômica e segura contra concorrência.

**Acceptance Criteria:**
- [ ] Migration cria `transactions` e `entries` (id, transaction_id, account_id, `amount_cents` com sinal); o banco rejeita valor 0 (`CHECK`) e conta inexistente (FK); índice em (account_id, data)
- [ ] Transação, seus 2 lançamentos e a atualização dos 2 saldos em `account_balances` acontecem na mesma transação de banco (tudo ou nada)
- [ ] Saldo atualizado com `UPDATE ... SET balance_cents = balance_cents + $1`, travando as duas contas sempre em ordem crescente de id (sem deadlock)
- [ ] Conta fechada é rejeitada com erro de domínio (422); conta inexistente com 404
- [ ] Teste de concorrência (`-race`, várias goroutines com transações cruzadas entre as mesmas contas) termina sem deadlock e com saldos corretos
- [ ] `make test` e `make lint` passam

### US-005: Lançar transação (API)
**Description:** Como usuário da API, quero registrar uma movimentação entre duas contas.

**Acceptance Criteria:**
- [ ] `POST /transactions` com `debit_account_id`, `credit_account_id`, `amount_cents`, `date`, `description` retorna 201 com os 2 lançamentos
- [ ] Mesmos padrões de erro do restante da API (400, 404, 422)
- [ ] `openapi.yaml` atualizado e validado
- [ ] `make test` e `make lint` passam

### US-006: Idempotência nas escritas
**Description:** Como cliente da API, quero repetir uma requisição sem duplicar a transação.

**Acceptance Criteria:**
- [ ] `POST /transactions` aceita o header `Idempotency-Key` (1 a 100 caracteres), guardado em `transactions.idempotency_key` (único, nulo permitido) com `request_hash`
- [ ] Mesma chave com o mesmo corpo retorna a resposta original (mesmo status e id), sem criar outra transação
- [ ] Mesma chave com corpo diferente retorna 422
- [ ] Duas requisições simultâneas com a mesma chave criam só uma transação (garantido por índice único, testado)
- [ ] Sem o header, o comportamento atual se mantém
- [ ] `make test` e `make lint` passam

### US-007: Estornar transação
**Description:** Como usuário da API, quero anular uma transação lançada por engano.

**Acceptance Criteria:**
- [ ] `POST /transactions/{id}/reversal` cria nova transação com débito e crédito invertidos e mesmo valor, ligada à original
- [ ] A transação original não é alterada
- [ ] Estornar duas vezes a mesma transação retorna 409 (garantido por índice único)
- [ ] Estornar um estorno retorna 422
- [ ] Transação inexistente retorna 404
- [ ] `make test` e `make lint` passam

### US-008: Saldo da conta em uma data
**Description:** Como usuário da API, quero saber o saldo de uma conta hoje ou em uma data passada.

**Acceptance Criteria:**
- [ ] `GET /accounts/{id}/balance` sem `as_of` lê o saldo materializado; com `as_of=YYYY-MM-DD` soma os lançamentos até a data, inclusive
- [ ] Sinal exibido segue o tipo: `asset` e `expense` crescem com débito; `liability`, `equity` e `income` crescem com crédito
- [ ] Resposta traz o tipo da conta e `balance_cents` já no sinal natural
- [ ] Teste cobrindo: conta sem movimento (0), várias transações, estorno anulando a original, data anterior ao primeiro lançamento
- [ ] `make test` e `make lint` passam

### US-009: Extrato paginado
**Description:** Como usuário da API, quero ver os lançamentos de uma conta em ordem.

**Acceptance Criteria:**
- [ ] `GET /accounts/{id}/entries?from=&to=&limit=&after=` lista lançamentos ordenados por data e id, paginação keyset com cursor opaco
- [ ] Cada item traz data, descrição, valor com sinal e saldo acumulado após o lançamento
- [ ] Filtro de período inclusivo; limite 1 a 100, padrão 20 (mesmas regras de hoje)
- [ ] `make test` e `make lint` passam

### US-010: Balancete
**Description:** Como usuário da API, quero conferir que o ledger inteiro está consistente.

**Acceptance Criteria:**
- [ ] `GET /trial-balance?as_of=` retorna o saldo de cada conta e o total geral
- [ ] O total de débitos é igual ao total de créditos (`balanced: true`); se não for, `balanced: false` e log de erro
- [ ] Também confere, conta a conta, saldo materializado = soma dos lançamentos; divergência aparece na resposta (`mismatched_accounts`) e em log de erro
- [ ] Teste de integração com várias transações e estornos confirmando `balanced: true`
- [ ] `make test` e `make lint` passam

### US-011: Conciliar parcela agendada com transação
**Description:** Como usuário, quero marcar uma parcela de despesa agendada como paga, gerando a transação correspondente no ledger.

**Acceptance Criteria:**
- [ ] `POST /expenses/{id}/payments` com `installment_number`, `debit_account_id`, `credit_account_id` cria a transação e registra o vínculo parcela-transação (índice único: uma parcela só é paga uma vez, senão 409)
- [ ] Valor e descrição vêm da despesa; a data é a de hoje ou `paid_on` informado
- [ ] Parcela inexistente, pulada ou de despesa cancelada retorna 422
- [ ] A criação da transação e do vínculo acontecem na mesma transação de banco
- [ ] `make test` e `make lint` passam

### US-012: Previsão mensal com realizado + projetado
**Description:** Como usuário, quero ver um mês com o que já foi pago e o que ainda está previsto, como na `ListMonthlyBills` do financial-reminder-api.

**Acceptance Criteria:**
- [ ] `GET /monthly-bills?month=YYYY-MM` retorna os itens do mês: parcelas pagas (com `transaction_id`) e parcelas projetadas ainda não pagas
- [ ] Item pago e projetado da mesma parcela aparece uma vez só (o pago vence)
- [ ] Cada item tem status `paid`, `pending` ou `overdue` (vencida e não paga, comparada com a data de hoje)
- [ ] Resposta traz totais `paid_cents`, `pending_cents`, `overdue_cents`
- [ ] Paginação por cursor (keyset), sem paginar em memória por offset
- [ ] Limite de consulta: no máximo 5 anos à frente (mesmo limite do Java)
- [ ] `make test` e `make lint` passam

### US-013: Endurecer o servidor HTTP
**Description:** Como operador, quero o servidor com timeouts e um healthcheck para rodar em Kubernetes.

**Acceptance Criteria:**
- [ ] `http.Server` com `ReadTimeout`, `WriteTimeout` e `IdleTimeout` definidos
- [ ] `GET /healthz` retorna 200 quando o banco responde ao ping e 503 quando não
- [ ] `pgx` atualizado para a última v5 e `go mod tidy` limpo
- [ ] `make test` e `make lint` passam

### US-014: Simplificar helpers HTTP
**Description:** Como desenvolvedor, quero menos código duplicado nos handlers.

**Acceptance Criteria:**
- [ ] `decodeJSONBody` e `decodeOptionalJSONBody` unificados em um único helper
- [ ] Sem mudança de comportamento: todos os testes existentes passam sem alteração
- [ ] `make test` e `make lint` passam

### US-015: Dockerfile e manifests Kubernetes
**Description:** Como operador, quero subir o ledger no mesmo cluster dos demais projetos do workspace.

**Acceptance Criteria:**
- [ ] `Dockerfile` multi-stage, imagem final mínima, roda como usuário não-root
- [ ] Manifests em `k8s/` (Deployment, Service, Secret/ConfigMap) com probes apontando para `/healthz`, seguindo o padrão dos outros projetos
- [ ] `DATABASE_URL` vem de Secret, nunca do repositório
- [ ] README atualizado com como construir e subir

## 5. Requisitos Funcionais

- FR-1: Toda transação tem exatamente 2 lançamentos (um débito e um crédito) de mesmo valor positivo, em contas diferentes e abertas.
- FR-2: O ledger é append-only: nenhuma linha de transação ou lançamento é atualizada ou apagada pela aplicação. A única tabela atualizada é a derivada `account_balances`.
- FR-3: Correção de erro só por estorno; uma transação é estornada no máximo uma vez, e um estorno não é estornável.
- FR-4: Os lançamentos são a fonte da verdade. O saldo atual fica em `account_balances` (derivado), atualizado na mesma transação de banco que grava os lançamentos; saldo em data passada é a soma dos lançamentos. Nenhuma rota edita saldo diretamente.
- FR-5: O balancete deve fechar em zero; `balanced: false` é tratado como incidente (log de erro).
- FR-6: `POST /transactions` aceita `Idempotency-Key`; mesma chave e mesmo corpo não duplicam.
- FR-7: Valores em centavos (`int64`), moeda única BRL, datas `YYYY-MM-DD` em UTC.
- FR-8: Uma parcela de despesa agendada vira no máximo uma transação.
- FR-9: Listagens usam paginação keyset com cursor opaco, limite 1 a 100, padrão 20.
- FR-10: Erros seguem o padrão atual: 400 requisição malformada, 404 não encontrado, 409 conflito, 422 regra violada.
- FR-11: Toda rota nova entra no `openapi.yaml`, validado por teste.

## 6. Non-Goals (fora de escopo)

- Usuários, autenticação, autorização, multi-tenant.
- Multimoeda e câmbio.
- Transações com mais de 2 lançamentos (partidas dobradas completas). O modelo deve permitir evoluir para isso depois.
- Edição ou exclusão de transações.
- Transações futuras/agendadas dentro do ledger (a agenda continua no módulo de despesas).
- CI no GitHub Actions e `golangci-lint` (segundo momento).
- Interface web, observabilidade além de logs, cache.

## 7. Considerações Técnicas

- Mantém o stack atual: `net/http`, `pgx`, `goose`, `slog`. Sem novas libs previstas.
- Pacotes: `internal/ledger` (contas, transações, saldo, estorno) e `internal/scheduling` (despesas agendadas, previsão, monthly-bills). `scheduling` importa `ledger`, nunca o contrário. `internal/postgres` e `internal/httpapi` atendem aos dois.
- **Comparação com `ListMonthlyBills` (Java):** a regra é a mesma ideia (itens reais + itens simulados, sem duplicar, ordenados, com totais pago/pendente/atrasado). Diferenças propostas para o Go:
  - Mês consultado é expandido a partir de cada despesa em O(1) para o período, sem iterar desde a primeira parcela (hoje `ScheduledPaymentsIn` itera do início).
  - Paginação keyset em vez de `offset` em memória.
  - "Real" é uma transação do ledger vinculada à parcela, em vez de uma tabela paralela de pagamentos.
- Concorrência: unicidade (nome de conta, estorno, idempotência, parcela paga) garantida por índice único no banco, e o código traduz o erro, como já é feito em `translateConstraintError`.

## 8. Métricas de Sucesso

- `make test` com `-race` passa, com testes de integração para cada invariante (fecha em zero, estorno único, idempotência concorrente).
- Teste de propriedade ou de tabela mostrando que, após N transações e estornos aleatórios, o balancete fecha em zero.
- Um desenvolvedor Java entende o fluxo de uma transação lendo o README e o pacote em menos de 15 minutos (critério subjetivo, validado por você).

## 9. Decisões de arquitetura (confirmadas com o autor)

1. **Lançamentos:** tabela `entries` com 2 linhas por transação e valor com sinal; permite evoluir para N lançamentos sem mudar o schema.
2. **Tipos de conta:** os 5 clássicos (`asset`, `liability`, `equity`, `income`, `expense`); o tipo só decide o sinal exibido.
3. **Saldo:** materializado em `account_balances` (cache derivado, atualizado na mesma transação) + reconciliação no balancete. Saldo em data passada usa `SUM`.
4. **Pacotes:** `internal/ledger` + `internal/scheduling`.
5. **Fechar conta:** exige saldo zero.
6. **Idempotência:** coluna `idempotency_key` + `request_hash` em `transactions`; cobre só `POST /transactions`.
7. **Contas iniciais:** nenhuma; o usuário cria todas.
8. **Ordem de implementação:** preparação primeiro (ver seção 10).

## 10. Ordem de implementação

0. **US-016** mover o código atual para `internal/scheduling`
1. US-014, US-013
2. US-001 a US-003 (contas)
3. US-004, US-017, US-005 a US-007 (transações, idempotência, estorno)
4. US-008 a US-010 (saldo, extrato, balancete)
5. US-011, US-012 (conciliação e monthly-bills)
6. US-015 (Docker/k8s)

### US-016: Mover despesas e previsão para `internal/scheduling`
**Description:** Como desenvolvedor, quero separar o módulo de despesas agendadas do ledger novo para que cada pacote tenha uma responsabilidade.

**Acceptance Criteria:**
- [ ] Todo o código atual de `internal/ledger` (despesas, recorrência, previsão, paginação, erros) vai para `internal/scheduling`, sem mudança de comportamento
- [ ] `internal/ledger` fica apenas com o que for compartilhado de fato (erros sentinela) ou vazio com `doc.go`
- [ ] Repositório de despesas continua em `internal/postgres`; handlers continuam em `internal/httpapi`
- [ ] `cmd/ledger/main.go`, README e a seção "Estrutura" atualizados
- [ ] Todos os testes existentes passam sem alterar asserts, apenas imports e nomes de pacote
- [ ] `make test` e `make lint` passam

## 11. Open Questions

- Nenhuma pendente de arquitetura. Novas dúvidas surgidas durante a implementação serão confirmadas com o autor antes de decidir.
