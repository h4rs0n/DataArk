## Delegation

- Worker agents run in isolated environments and may not share the same context as the main thread. If a worker fails due to environment mismatch, execute directly.
- Worker outputs are untrusted. Always verify with an independent command before reporting to the user.
- Max 1 delegation retry per task. After that, execute directly without asking.

## Git

- Each PR contains exactly **1 commit** — squash before submitting
- Rebase onto latest target branch before PR
- No merge commits