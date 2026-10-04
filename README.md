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

`conn` is a `github.com/go-remoteexec/transport.Connection` — local, SSH
or WinRM. The WinRM one reaches a Windows host, though most modules here
compose POSIX shell and so will not run against one; the `ansible.windows`
family is not ported.
Unlike real Ansible, a module here runs its logic on the control node and
reaches the target only through the connection's `Exec`/`Put`/`Fetch`
primitives — no Python, no script copied to the target. `reg.Names()` lists
every registered module name; `reg.Register` adds or overrides one.

## Security

A module here composes a shell command, so a value that reaches a command
string reaches a shell. Two controls, both mechanical rather than careful:

- **Quoting.** Every interpolated value goes through this package's
  `shellQuote` — 1 715 call sites. *Being careful* does not scale to 608
  files, and a missed site is indistinguishable from a deliberate one until
  somebody looks.
- **Credentials in the environment**, via `transport`'s `ExecWithEnv`, which
  is what real Ansible does with `run_command`'s `environ_update`. Never a
  secret in `argv`, where `ps` shows it to every local user.

An audit of this library found one breach of each, both fixed in v0.80.0 and
v0.80.1: `async_status`'s `jid` was interpolated **unquoted** into an
`rm -rf`, and `keyring` built its passwords into a *two-statement* command,
where the shell cannot `exec` away and holds them in its `argv`. The port's
remaining unavoidable exposures — macOS `security`'s own `argv`, and two
vendor CLIs with an irreducible token flag — are named, with the measurement
behind each.

The full writeup, including the measured `ps`-visibility boundary and the
three lessons that generalise beyond these two defects, is on
[the Security page](https://go-ansible.github.io/docs/security/).
