# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.1.0] - 2026-09-17

First release. The API is not frozen yet: it stays on `v0.x` until it has been
used in anger and the rough edges are known.

### Added

**Strategies** — randomise one duration.

- `Full`, `Equal`, `Proportional`, `Decorrelated` and `None`.
- `Early`, which never exceeds the duration given, for delays protecting a
  deadline: lease renewal, token refresh, cache expiry.
- Randomness enters through a `Source` interface, so tests can be
  deterministic; `nil` means the default process-wide source.

**Backoff** — turn a strategy into a sequence.

- Geometric growth from a base, clamped to a max, jittered on the way out.
- `WithBase`, `WithMax`, `WithMultiplier`, `WithStrategy`, `WithMaxRetries`,
  `WithMaxElapsed`, `WithMaxRetryAfter`, `WithOnRetry`.
- Invalid options fall back to documented defaults rather than failing, so
  `New` never returns an error.

**Retry** — run a function against a Backoff.

- `Do` and `DoValue[T]`, both context-aware.
- `Permanent` to stop immediately; `RetryAfter` to let a server name its own
  delay.

**Scheduling** — spread periodic work across a fleet.

- `Ticker`, shaped like `time.Ticker`, and `Every` for a context-bound loop.
- `Wait`, a jittered and cancellable sleep.

**HTTP**

- `Transport`, an `http.RoundTripper` that retries idempotent requests,
  honours `Retry-After`, replays request bodies only when it safely can, and
  returns the last response rather than an error when retries run out.

[v0.1.0]: https://github.com/bakhod1r/jitterx/releases/tag/v0.1.0
