---
description: Create a safe, reversible database migration
argument-hint: <snake_case_name>
---

Read `docs/DATABASE.md` §Migrations first.

1. Run `make migrate-new name=$ARGUMENTS`.
2. Write the `up` SQL. Ask yourself out loud and answer in your response:
   - Is this zero-downtime? If it renames, retypes, or drops, split into expand → backfill →
     contract across releases.
   - Does it lock a big table? Use `CREATE INDEX CONCURRENTLY` (no transaction) and
     `SET lock_timeout`.
   - Does it need a backfill? Batch it (`LIMIT 1000` loop), never one giant `UPDATE`.
3. Write a real `down`. If truly irreversible, say so in a comment and document recovery.
4. Update the repository queries and any `sqlc` models (`make gen`). Files live in `backend/migrations/`.
   `delivery_attempts` is partitioned by month: alter the parent table; indexes must suit partitions.
5. `make migrate-up && make migrate-down && make migrate-up` locally to prove reversibility.
6. Show me: the SQL, the estimated lock impact, and the rollback plan.

Never edit an existing migration file.
