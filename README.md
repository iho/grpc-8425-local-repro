# Local repro: grpc-go transport Close hang (#8425)

Deterministic demo of the failure class behind:

- Issue: [kubernetes/kubernetes#140911](https://github.com/kubernetes/kubernetes/issues/140911)
- Upstream: [grpc/grpc-go#8425](https://github.com/grpc/grpc-go/issues/8425) / fix [#8534](https://github.com/grpc/grpc-go/pull/8534) (v1.77+)
- k8s release PRs: [1.35](https://github.com/kubernetes/kubernetes/pull/141126) · [1.34](https://github.com/kubernetes/kubernetes/pull/141127)

## What it shows

| grpc version | `transport.Close` on mute peer | k8s line |
|--------------|--------------------------------|----------|
| **v1.72.2** | **blocks** (&gt;8s) — vulnerable | release-1.34 / 1.35 |
| **v1.79.3** | returns in **~1.0s** — fixed | release-1.36 + those PRs |

### Mechanism (under test)

1. Build an HTTP/2 client transport whose `net.Conn` finishes the server SETTINGS preface, then **never returns more data**.
2. `Close()` on that conn **does not** unblock `Read` (blackhole / half-open).
3. `http2Client.Close` waits on `<-readerDone` after shutting down the transport.
4. **v1.72.x** never sets a read deadline → wait hangs.  
   **v1.77+** (`#8534`) calls `SetReadDeadline(now+1s)` → reader exits → Close returns.

This does **not** replay Calico FV end-to-end. It **does** show the exact Close/readerDone behavior fixed by the version bump that those release PRs ship.

## Run

```bash
git clone https://github.com/iho/grpc-8425-local-repro.git
cd grpc-8425-local-repro
./run.sh
```

Needs: `git`, `go` (1.24+). Network access to clone `grpc/grpc-go` tags.

### Expected output (abbreviated)

```text
# v1.72.2
RESULT: Close BLOCKED > 8s (VULNERABLE pattern)
--- FAIL: TestMutePeerCloseLatency

# v1.79.3
RESULT: Close returned in 1.001s
=> FIXED pattern (~1s SetReadDeadline)
--- PASS: TestMutePeerCloseLatency
```
