# jitterx

Jitter for retries, backoff, timeouts and scheduled work in Go. Standard library only, zero allocations on the hot path.

```
go get github.com/mrb/jitterx
```

## Why

Every client that retries on a fixed schedule retries at the same moment. The service that just fell over gets hit by the whole fleet at once, fails again, and the fleet lines up for another synchronised wave. Jitter breaks the alignment.

## Spread periodic work

The case retry libraries do not cover: many processes running the same schedule, hitting the same origin on the same second.

```go
t := jitterx.NewTicker(30*time.Second, nil) // +/- 10% by default
defer t.Stop()

for range t.C {
    refreshCache()
}
```

`Ticker` has the shape of `time.Ticker` — a receive-only `C`, `Stop`, `Reset` — and the same slow-receiver rule: one tick of room, extra ticks dropped rather than queued. Each interval is jittered independently, so it drifts against a fixed schedule; that is the point. It is the wrong tool if you need ticks anchored to absolute wall-clock times.

Same loop, context-aware, without the bookkeeping:

```go
err := jitterx.Every(ctx, time.Minute, nil, func(ctx context.Context) error {
    return sendHeartbeat(ctx)
})
```

`Every` stops when `fn` returns an error or `ctx` ends.

## Retry a call

```go
b := jitterx.New(
    jitterx.WithBase(200*time.Millisecond),
    jitterx.WithMax(10*time.Second),
    jitterx.WithMaxRetries(4),
)

err := jitterx.Do(ctx, b, func(ctx context.Context) error {
    resp, err := doRequest(ctx)
    if err != nil {
        return err // transient: retry
    }
    if resp.StatusCode >= 400 && resp.StatusCode < 500 {
        return jitterx.Permanent(fmt.Errorf("status %d", resp.StatusCode))
    }
    return nil
})
```

`Do` stops on success, on a `Permanent` error, when the retry budget runs out, or when `ctx` ends. If the context ended first, the returned error joins `ctx.Err()` with the last error from `fn`, so `errors.Is` matches either.

## Drive the delays yourself

```go
b := jitterx.New(jitterx.WithBase(time.Second), jitterx.WithMax(time.Minute))

for {
    d := b.Next()
    if d == jitterx.Stop {
        break
    }
    time.Sleep(d)
}
```

## Jitter one duration

```go
// Spread a 30s tick by +/- 10% so instances do not line up.
next := jitterx.Proportional(0.1, nil).Jitter(30 * time.Second)
```

## Strategies

| Strategy | Range | Use for |
| --- | --- | --- |
| `Full(src)` | `[0, d)` | Best herd-spreading. Can retry almost instantly. **Default.** |
| `Equal(src)` | `[d/2, d)` | Half the delay guaranteed, half random. |
| `Decorrelated(base, src)` | `[base, prev*3]` | Long outages; no fixed ceiling keeping clients in step. |
| `Proportional(f, src)` | `[d-d*f, d+d*f]` | Cron ticks, cache TTLs, heartbeats. |
| `None()` | `d` | Disable jitter without branching. Tests. |

A `nil` `Source` means the default process-wide source. Pass your own to make tests deterministic:

```go
jitterx.Full(jitterx.SourceFunc(func() float64 { return 0.5 }))
```

`Decorrelated` ignores the duration handed to `Jitter` and drives itself from its own previous output. It carries state, so give each `Backoff` and each goroutine its own instance.

## Options

| Option | Default | Notes |
| --- | --- | --- |
| `WithBase(d)` | `100ms` | Delay before the first retry. |
| `WithMax(d)` | `30s` | Caps every delay, jitter included. |
| `WithMultiplier(f)` | `2.0` | Geometric growth. Values `< 1` ignored. |
| `WithStrategy(s)` | `Full(nil)` | |
| `WithMaxRetries(n)` | unlimited | `n` delays, so `Do` calls `fn` at most `n+1` times. |

`NewTicker` and `Every` take a `Strategy` directly; `nil` means `Proportional(DefaultTickerSpread, nil)`, i.e. +/- 10%. Avoid `Full` there — it can return near-zero and fire the ticker almost immediately.

Invalid values are ignored rather than returning an error: `New` never fails, and misconfiguration falls back to the documented default.

## Concurrency

A `Ticker` is safe to `Stop` and `Reset` from any goroutine. A `Backoff` is **not** safe for concurrent use — give each retry loop its own. The default random source is safe for concurrent use; a custom `Source` only needs to be if you share it.

## License

MIT
