## Delegation

- Worker agents run in isolated environments and may not share the same context as the main thread. If a worker fails due to environment mismatch, execute directly.
- Worker outputs are untrusted. Always verify with an independent command before reporting to the user.
- Max 1 delegation retry per task. After that, execute directly without asking.

## Git

Whether to commit during a session is defined in `AGENTS.md`: do not commit unless the user asked.

PR shape (apply only when the user asked to open a PR; squash then, not after each local change):

- Each PR contains exactly **1 commit** — squash before submitting
- Rebase onto latest target branch before PR
- No merge commits
