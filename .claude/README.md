# `.claude/`

| Path | Purpose |
| --- | --- |
| `settings.json` | Shared permissions + env. Committed. |
| `settings.local.json` | Personal overrides. **Gitignored.** |
| `commands/` | Slash commands — repeatable workflows |
| `agents/` | Subagents — isolated context for review/test/debug |

Editing `settings.json` deny-list requires a PR review. `backend/migrations/**/*.up.sql` is denied on
purpose: new migrations are created through `make migrate-new`, which also writes the `down` file.
