# Docker

## Runtime: tini

The Docker image uses [tini](https://github.com/krallin/tini) as the init process (PID 1):

```dockerfile
ENTRYPOINT ["tini", "--"]
CMD ["mails", "serve"]
```

tini is a minimal init system that:

- **Handles signals** — Receives SIGTERM/SIGINT on container stop and forwards them to the child process so the app can shut down cleanly.
- **Reaps zombies** — Cleans up orphaned child processes (zombies). The app spawns `readpst` from pst-utils for PST imports; those subprocesses would otherwise become zombies when they exit.

Without tini, the main process would be PID 1 and might not reap zombies or handle signals correctly. See [tini on GitHub](https://github.com/krallin/tini) for details.

## Runtime Dependencies

| Package       | Purpose                                    |
| ------------- | ------------------------------------------ |
| ca-certificates | HTTPS for OAuth and IMAP/POP3 connections |
| tini         | Init process (signal handling, zombie reaping) |
| pst-utils    | `readpst` for PST/OST file import          |

## Build

- **BuildKit** — `# syntax=docker/dockerfile:1.4` enables cache mounts for faster rebuilds.
- **Cache mounts** — `--mount=type=cache,target=/go/pkg/mod` and `--mount=type=cache,target=/root/.cache/go-build` cache Go modules and build output.
- **Multi-stage** — Builder stage (golang) compiles the binary; runtime stage (debian-slim) ships only the binary and minimal deps.

See [CONTRIBUTING.md](../CONTRIBUTING.md#docker) for usage commands.
