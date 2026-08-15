# Transports & security

The KittyTK display protocol is transport-agnostic: the wire language is
identical whether it travels over a unix socket, a plaintext TCP
connection, or a TLS connection. Only the *endpoint* and the *dial*
differ. The Go, Python, and C clients all implement the same scheme, and
all run on Windows, Linux, and macOS.

## Endpoints

An endpoint is either a filesystem path or a URL:

| Endpoint                         | Transport                                   |
| -------------------------------- | ------------------------------------------- |
| `/run/kittytk/display-0.sock`    | unix socket (a bare path; the default)      |
| `unix:/run/kittytk/display-0.sock` | unix socket (explicit)                    |
| `tcp://host:port`                | plaintext TCP — loopback / trusted LAN only |
| `tls://host:port`                | TLS over TCP — for real remote use          |

`tcp://` / `tls://` default to port **9797** when none is given.

The endpoint is chosen by `$KITTYTK_DISPLAY` (host and clients both read
it); unset, it defaults to `<runtime>/kittytk/display-0.sock`, a unix
socket. On Windows, where `AF_UNIX` may be unavailable (notably from
Python), use a loopback `tcp://127.0.0.1:9797` endpoint instead.

### Host

```go
// unix (unchanged):
display.Serve(desktop, client.DefaultEndpoint())

// any transport, with auth + the interactive prompt:
display.ServeConfig(desktop, display.DefaultConfig(desktop, "tls://0.0.0.0:9797"))
```

### Clients

```go
client.Dial("tls://host:9797", "My App", nil)          // Go
```
```python
kittytk.dial("tls://host:9797", "My App")               # Python
```
```c
kt_dial("tls://host:9797", "My App");                  /* C, built -DKT_TLS */
```

`Dial`, `dial`, and `kt_dial` keep their old signatures; a bare path is
still a unix socket, so existing code is unaffected.

## TLS is PKI-free (trust on first use)

There is no certificate authority. The host presents a **persistent
self-signed certificate**; a client pins its SHA-256 fingerprint the
first time it connects (recorded in `known_hosts`) and checks it on every
reconnect. A changed fingerprint is refused loudly — the SSH
"host identity changed" defense against a man-in-the-middle. To be sure
on the very first connect, exchange the fingerprint out of band and
pre-seed `known_hosts` (the host prints its fingerprint on startup).

`$KITTYTK_INSECURE=1` disables pinning (diagnostics only).

## Who may connect — the host asks

`tls://` is **mutual TLS**: the client also presents a persistent
self-signed identity (generated once, like an SSH key), and the host
learns *its* fingerprint. Because the host is the only user interface, the
host is what authorizes: on a non-local connection it shows a prompt
identifying the app by name and the client by fingerprint. Decisions are
keyed by **(fingerprint, app name)** so a trusted client cannot silently
present an app it was not approved for:

```
Accept connection for "App Name" from sha256:…?
  Yes → Once Only · Always · Always for All Apps
  No  → Not Now · Never for this App · Block Client
```

| Button              | Effect                                            |
| ------------------- | ------------------------------------------------- |
| Once Only           | admit this session                                |
| Always              | admit + remember this fingerprint **for this app**|
| Always for All Apps | admit + remember this fingerprint **for any app** |
| Not Now             | refuse this session                               |
| Never for this App  | refuse + remember (fingerprint, app)              |
| Block Client        | refuse + remember the fingerprint (any app)       |

The persistent choices live in the `authorizations` file; **deny wins
over allow**. A future Psi-menu "Pre-Trusted Clients Only" toggle
(`Server.SetPreTrustedOnly`) auto-refuses anything not already trusted,
without prompting.

Local connections (unix socket or loopback) are same-machine and trusted
automatically — no prompt — unless the host sets `Config.PromptLocal`.

### Token (headless bypass)

A host with no user to click the prompt (CI, a service) can set
`Config.Token`; any client presenting a matching `$KITTYTK_TOKEN` in its
handshake is admitted. Over `tls://` the token is unsniffable. It never
gates local connections.

## Files & environment

Per-machine config lives in `<config>/kittytk/`, where `<config>` is
`$XDG_CONFIG_HOME`, else `%APPDATA%` on Windows, else `~/.config` — the
same rule in all three clients.

| Path                          | What                                          |
| ----------------------------- | --------------------------------------------- |
| `<config>/kittytk/identity.pem`      | client identity (key + cert)           |
| `<config>/kittytk/known_hosts`       | pinned host fingerprints               |
| `<config>/kittytk/host_identity.pem` | host's persistent self-signed cert     |
| `<config>/kittytk/authorizations`    | remembered allow/deny decisions        |

| Env var               | Purpose                                             |
| --------------------- | --------------------------------------------------- |
| `KITTYTK_DISPLAY`     | endpoint (path or `tcp://` / `tls://` URL)          |
| `KITTYTK_TOKEN`       | handshake token (client sends, host may require)    |
| `KITTYTK_IDENTITY`    | override the client identity PEM path               |
| `KITTYTK_KNOWN_HOSTS` | override the pin store path                         |
| `KITTYTK_HOST_IDENTITY` | override the host cert PEM path (host side)        |
| `KITTYTK_AUTHORIZATIONS` | override the decisions file (host side)          |
| `KITTYTK_INSECURE`    | `1` disables TLS pinning (diagnostics)              |

## Cross-platform notes

- **Go** — fully portable; `tcp://`/`tls://` everywhere, `AF_UNIX` on
  Windows 10+.
- **Python** — portable; `tls://` needs the `cryptography` package or the
  `openssl` CLI to mint the client identity (else point `$KITTYTK_IDENTITY`
  at an existing PEM). `AF_UNIX` may be absent on Windows — use `tcp://`.
- **C** — compiles natively on Windows (Winsock2 + Win32 threads) and
  POSIX. The default build (unix + `tcp://`) needs only libc + pthreads;
  `tls://` is opt-in via `-DKT_TLS` and links OpenSSL.
