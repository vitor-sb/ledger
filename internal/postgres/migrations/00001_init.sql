-- +goose Up
-- Append-only ledger: expenses, plus records added on top of them (cancellation, skipped installments).
-- The application never edits or deletes rows.
CREATE TABLE expenses (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title             VARCHAR(30) NOT NULL,
    amount_cents      BIGINT      NOT NULL CHECK (amount_cents > 0),
    first_due_date    DATE        NOT NULL,
    recurrence        TEXT        NOT NULL CHECK (recurrence IN
                      ('UNICA','DIARIA','SEMANAL','QUINZENAL','MENSAL','TRIMESTRAL','SEMESTRAL','ANUAL')),
    installment_count INT         NOT NULL DEFAULT 0 CHECK (installment_count >= 0), -- 0 = open-ended
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE expense_cancellations (
    expense_id     BIGINT      PRIMARY KEY REFERENCES expenses (id),
    effective_date DATE        NOT NULL,
    cancelled_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE skipped_installments (
    expense_id         BIGINT      NOT NULL REFERENCES expenses (id),
    installment_number INT         NOT NULL CHECK (installment_number > 0),
    skipped_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (expense_id, installment_number)
);

-- +goose Down
DROP TABLE skipped_installments;
DROP TABLE expense_cancellations;
DROP TABLE expenses;
