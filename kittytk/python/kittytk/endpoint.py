"""Transport addressing, TLS trust-on-first-use, and the client identity.

The wire language is identical over every transport; only the endpoint
string and the dial differ:

    unix:/run/kittytk/display-0.sock   an explicit unix socket
    /run/kittytk/display-0.sock        a bare path is unix (back-compat)
    tcp://host:port                    plaintext TCP (loopback/trusted LAN)
    tls://host:port                    TLS over TCP (real remote)

TLS is PKI-free: the host's certificate fingerprint is pinned on first
connect (SSH known_hosts style) and checked on reconnect. tls:// is
mutual TLS, so the client presents a persistent self-signed identity
whose fingerprint the host approves. The rules here mirror the Go and C
clients byte-for-byte so all three share one per-machine config.
"""

from __future__ import annotations

import hashlib
import os
import socket
import ssl
import subprocess
import sys
import tempfile

DEFAULT_TCP_PORT = "9797"

KNOWN_HOSTS_ENV = "KITTYTK_KNOWN_HOSTS"
IDENTITY_ENV = "KITTYTK_IDENTITY"
INSECURE_ENV = "KITTYTK_INSECURE"


def config_dir() -> str:
    """<config>/kittytk: $XDG_CONFIG_HOME, else %APPDATA% on Windows,
    else ~/.config (mirrors the Go and C clients)."""
    x = os.environ.get("XDG_CONFIG_HOME")
    if x:
        base = x
    elif sys.platform == "win32" and os.environ.get("APPDATA"):
        base = os.environ["APPDATA"]
    else:
        base = os.path.join(os.path.expanduser("~"), ".config")
    return os.path.join(base, "kittytk")


def known_hosts_path() -> str:
    return os.environ.get(KNOWN_HOSTS_ENV) or os.path.join(config_dir(), "known_hosts")


def identity_path() -> str:
    return os.environ.get(IDENTITY_ENV) or os.path.join(config_dir(), "identity.pem")


def _with_port(addr: str) -> str:
    host, sep, port = addr.rpartition(":")
    if sep and port.isdigit():
        return addr
    return addr + ":" + DEFAULT_TCP_PORT


def parse_endpoint(s: str):
    """(network, address, use_tls, host). A bare value is a unix path."""
    if s.startswith("unix:"):
        return ("unix", s[len("unix:"):], False, "")
    if s.startswith("tcp://"):
        return ("tcp", _with_port(s[len("tcp://"):]), False, "")
    if s.startswith("tls://"):
        addr = _with_port(s[len("tls://"):])
        return ("tcp", addr, True, addr.rpartition(":")[0])
    return ("unix", s, False, "")


def _is_truthy(v) -> bool:
    return str(v).strip().lower() in ("1", "true", "yes", "on")


def fingerprint_sha256(der: bytes) -> str:
    return "sha256:" + hashlib.sha256(der).hexdigest()


def _lookup_pin(path: str, hostport: str):
    try:
        with open(path, "r") as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#"):
                    continue
                parts = line.split()
                if len(parts) >= 2 and parts[0] == hostport:
                    return parts[1]
    except FileNotFoundError:
        return None
    return None


def _add_pin(path: str, hostport: str, fp: str):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "a") as f:
        f.write("%s %s\n" % (hostport, fp))
    try:
        os.chmod(path, 0o600)
    except OSError:
        pass


def _verify_pin(path: str, hostport: str, fp: str):
    pinned = _lookup_pin(path, hostport)
    if pinned is None:
        _add_pin(path, hostport, fp)
        sys.stderr.write("kittytk: pinned new host %s %s\n" % (hostport, fp))
        return
    if pinned != fp:
        raise ConnectionError(
            "host identity for %s changed!\n  pinned %s\n  got    %s\n"
            "if this is expected, remove that line from %s"
            % (hostport, pinned, fp, path))


# --- client identity certificate (mutual TLS) ----------------------------

def ensure_identity(path: str) -> str:
    """Return a PEM path holding this client's persistent key+cert,
    generating it on first use."""
    if os.path.exists(path):
        return path
    os.makedirs(os.path.dirname(path), exist_ok=True)
    pem = _generate_identity_pem()
    with open(path, "wb") as f:
        f.write(pem)
    try:
        os.chmod(path, 0o600)
    except OSError:
        pass
    return path


def _generate_identity_pem() -> bytes:
    # Prefer the cryptography package; fall back to the openssl CLI if it
    # is missing OR broken. A partial/mismatched install can raise more
    # than ImportError - even a Rust-binding panic (BaseException) - so
    # this fallback is deliberately broad.
    try:
        return _gen_with_cryptography()
    except BaseException:
        return _gen_with_openssl()


def _gen_with_cryptography() -> bytes:
    import datetime

    from cryptography import x509
    from cryptography.hazmat.primitives import hashes, serialization
    from cryptography.hazmat.primitives.asymmetric import ec
    from cryptography.x509.oid import NameOID

    key = ec.generate_private_key(ec.SECP256R1())
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "kittytk-client")])
    now = datetime.datetime.utcnow()
    cert = (
        x509.CertificateBuilder()
        .subject_name(name)
        .issuer_name(name)
        .public_key(key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now - datetime.timedelta(hours=1))
        .not_valid_after(now + datetime.timedelta(days=7300))
        .sign(key, hashes.SHA256())
    )
    key_pem = key.private_key_bytes(
        serialization.Encoding.PEM,
        serialization.PrivateFormat.TraditionalOpenSSL,
        serialization.NoEncryption(),
    )
    return key_pem + cert.public_bytes(serialization.Encoding.PEM)


def _gen_with_openssl() -> bytes:
    with tempfile.TemporaryDirectory() as d:
        key = os.path.join(d, "k.pem")
        crt = os.path.join(d, "c.pem")
        try:
            subprocess.run(
                ["openssl", "req", "-x509", "-newkey", "ec",
                 "-pkeyopt", "ec_paramgen_curve:prime256v1", "-nodes",
                 "-keyout", key, "-out", crt, "-days", "7300",
                 "-subj", "/CN=kittytk-client"],
                check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        except (OSError, subprocess.CalledProcessError) as e:
            raise RuntimeError(
                "cannot create a TLS client identity: install the "
                "'cryptography' package or the openssl CLI, or point "
                "$KITTYTK_IDENTITY at an existing key+cert PEM") from e
        with open(key, "rb") as f:
            kb = f.read()
        with open(crt, "rb") as f:
            cb = f.read()
        return kb + cb


def _tofu_context(identity: str) -> ssl.SSLContext:
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    ctx.check_hostname = False           # verification replaced by pinning
    ctx.verify_mode = ssl.CERT_NONE
    ctx.load_cert_chain(ensure_identity(identity))
    return ctx


def connect(endpoint: str, *, insecure=False, known_hosts=None,
            ssl_context=None) -> socket.socket:
    """Dial the endpoint, wrapping tls:// in a pinned mutual-TLS session.
    Returns a connected socket the caller reads/writes like any other."""
    network, address, use_tls, host = parse_endpoint(endpoint)

    if network == "unix":
        if not hasattr(socket, "AF_UNIX"):
            raise OSError(
                "unix sockets are unavailable on this platform; "
                "use a tcp:// or tls:// endpoint")
        s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        s.connect(address)
        return s

    host_only, port = address.rsplit(":", 1)
    raw = socket.create_connection((host_only, int(port)))
    if not use_tls:
        return raw

    insecure = insecure or _is_truthy(os.environ.get(INSECURE_ENV, ""))
    ctx = ssl_context or _tofu_context(identity_path())
    ss = ctx.wrap_socket(raw, server_hostname=host or host_only)
    if ssl_context is None and not insecure:
        der = ss.getpeercert(binary_form=True)
        _verify_pin(known_hosts or known_hosts_path(), address,
                    fingerprint_sha256(der))
    return ss
