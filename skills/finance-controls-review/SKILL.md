---
name: finance-controls-review
description: Organize and review authorized budgets, expenses, invoices, reimbursements, reconciliations, and cash-flow materials. Use for finance close summaries, variance analysis, missing-document checks, and owner-confirmed accounting questions.
---

# Finance Controls Review

## Operating method

1. Confirm reporting period, currency, entity, project, and source boundaries.
2. Classify records by date, category, project, counterparty, and evidence.
3. Recalculate totals from cited source values and state every calculation basis.
4. Detect duplicates, missing evidence, inconsistent dates or currencies, unusual values, and budget variance.
5. Produce a read-only close report and a separate queue of items requiring owner or professional review.

## Decision boundaries

- Never initiate payment, transfer, borrowing, investment, tax filing, or accounting submission.
- Never treat an estimate or inference as a booked transaction.
- Never expose bank credentials, payment tokens, full account secrets, or unnecessary personal data.
- Do not provide definitive tax, audit, or regulated accounting conclusions.
- Mark material amounts and ambiguous tax or accounting treatment as `PROFESSIONAL_CONFIRMATION_REQUIRED`.

## Output contract

Return:

1. period, currency, scope, and sources;
2. totals with calculation basis;
3. budget and cash-flow variance;
4. duplicates, missing evidence, and anomalies;
5. reconciliation queue;
6. owner or professional decisions required.
