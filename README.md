# Local repro: grpc-go transport Close hang (#8425)

Maps to [kubernetes/kubernetes#140911](https://github.com/kubernetes/kubernetes/issues/140911)
and release-branch PRs bumping grpc on 1.34/1.35.

## What it shows

| grpc version | `transport.Close` on mute peer |
|--------------|--------------------------------|
| **v1.72.2** (k8s 1.34/1.35) | **blocks** (>8s) — vulnerable |
| **v1.79.3** (k8s 1.36 / our PRs) | returns in **~1.0s** — fixed |

Mechanism under test (same as production bug class):

1. Build an HTTP/2 client transport whose `net.Conn` finishes the server SETTINGS preface, then **never returns more data**.
2. `Close()` on that conn **does not** unblock `Read` (blackhole / half-open).
3. `http2Client.Close` waits on `<-readerDone` after shutting down the transport.
4. **v1.72.x** never sets a read deadline → wait hangs.  
   **v1.77+** (`#8534`) calls `SetReadDeadline(now+1s)` → reader exits → Close returns.

This does **not** replay Calico FV end-to-end; it **does** demonstrate the exact Close/readerDone failure fixed by the version bump.

## Run

```bash
./run.sh
```

Needs: `git`, `go` (1.24+).
