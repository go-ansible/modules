# modules

Ansible module execution protocol plus the core module library.

Part of [go-ansible](https://github.com/go-ansible) — a pure-Go (CGO=0),
functional-parity port of [Ansible](https://www.ansible.com/).

[![CI](https://github.com/go-ansible/modules/actions/workflows/ci.yml/badge.svg)](https://github.com/go-ansible/modules/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/go-ansible/modules.svg)](https://pkg.go.dev/github.com/go-ansible/modules)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue.svg)](LICENSE)

## Usage

```go
reg := modules.Default() // pre-populated with the built-in module set

res, err := reg.Run(ctx, "copy", conn, map[string]any{
    "src": "app.conf", "dest": "/etc/app.conf", "mode": "0644",
})
if res.Failed {
    // res.Msg explains why
}
```

`conn` is a `github.com/go-remoteexec/transport.Connection` (local or SSH).
Unlike real Ansible, a module here runs its logic on the control node and
reaches the target only through the connection's `Exec`/`Put`/`Fetch`
primitives — no Python, no script copied to the target. `reg.Names()` lists
every registered module name; `reg.Register` adds or overrides one.
