// Package jitterx adds randomness to retries, backoff intervals, timeouts and
// scheduled work, so independent callers stop lining up on the same instant.
//
// Three pieces, each usable on its own:
//
//   - A [Strategy] randomises a single duration. [Full], [Equal],
//     [Proportional] and [Decorrelated] cover the usual choices.
//   - A [Backoff] turns a strategy into a sequence: geometric growth from a
//     base, clamped to a max, jittered on the way out.
//   - [Do] runs a function against a Backoff until it succeeds, is told to
//     stop, or the context ends.
//   - [Wait] is the primitive under both: a jittered, cancellable sleep.
//   - [Ticker] and [Every] put the same jitter on periodic work — cache
//     refreshes, heartbeats, lease renewals, reconcile loops — which is where
//     a fleet on a shared schedule hurts most.
//
// Picking a strategy:
//
//   - [Full] spreads a thundering herd best, but can retry almost instantly.
//   - [Equal] keeps half the delay guaranteed and randomises the rest.
//   - [Decorrelated] climbs and drops without a fixed schedule; good for long
//     outages where a strict ceiling would keep every client synchronised.
//   - [Proportional] keeps an interval roughly intact, for cron ticks, cache
//     TTLs and heartbeats.
//   - [Early] never exceeds the duration given, for delays protecting a
//     deadline: lease renewals, token refreshes, cache expiry.
//
// The package has no dependencies beyond the standard library. A [Backoff] is
// not safe for concurrent use; give each retry loop its own.
package jitterx
