# spec/ — the API contract

vmtool is **spec-first**. `openapi/vmtool.yaml` is the source of record for every HTTP
route, payload, and status code. Code follows the spec; the spec does not follow
a handler.

## Layout

| Path | Contract |
|---|---|
| `openapi/vmtool.yaml` | Every HTTP endpoint `vmtool server` registers. oapi-codegen generates models, a stdlib `ServerInterface`, a strict wrapper, an embedded spec, and a test client (`internal/api`). |

Interactive SSH from CLI (`vmtool ssh`) and TUI (`t`) execs `ssh` locally and
is not an HTTP operation. The browser/remote handshake is `GET /vms/{name}/console`
(101 Switching Protocols); frame the PTY bytes and resize JSON in that
operation's description, not as JSON schemas.

## Codegen workflow

```bash
go generate ./internal/api/...
scripts/verify-generate.sh   # regenerate + fail if internal/api is dirty
```

Run generate after editing the YAML and commit `internal/api/api.gen.go`.

## Coverage policy

**Every HTTP endpoint appears in `openapi/vmtool.yaml`.** Paths are literal and
`servers[0].url` is `/`, so the spec never lies about a real request path.

That includes the WebSocket upgrade handshake (`GET /vms/{name}/console`, 101
plus the usual error statuses). The framing after 101 is described in prose on
the operation. "It's not JSON" is not a reason to exclude it. The generated
strict handler cannot hijack a connection; the server implements this one
operation on `ServerInterface` (`OpenVMConsole(w, r, name, params)`) and the
rest on `StrictServerInterface`.

An exclusion requires a compelling technical reason and a row in the register
below. Enforced by `internal/server/specparity_test.go` once REST is on the
generated mux (see `docs/design/spec-first.md` PR 3).

## Exclusion register

| Route | Reason |
|---|---|
| *(empty)* | No routes are outside the spec. If oapi-codegen v2.4.1 emits a catch-all `GET /` that maps `/nope` to docs, move `getDocs` / `getOpenAPISpec` off the generated mux and record them here. |

## URL versioning

Paths are **unversioned** (`/vms`, `/pools`, …). vmtool is a local single-binary
tool with no lagging field clients. Revisit `/api/v1` only when a second consumer
cannot upgrade in lockstep.

## GET / pattern

PR1 records the exact std-http pattern oapi-codegen emits for the root after
the first generate. Update this line when known:

| Operation | Expected ServeMux pattern |
|---|---|
| `getDocs` | `GET /{$}` — Swagger UI (not a catch-all) |
| `getOpenAPISpec` | `GET /openapi.yaml` |
| `getOpenAPIJSON` | `GET /openapi.json` |
