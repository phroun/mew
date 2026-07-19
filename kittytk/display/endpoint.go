package display

// Host-side endpoint parsing, mirroring the client's rule: a bare value
// is a unix socket path (back-compat), unix:/tcp://tls:// select the
// transport.

import (
	"net"
	"strings"
)

const defaultTCPPort = "9797"

type endpoint struct {
	network string // "unix" | "tcp"
	address string // path or host:port
	useTLS  bool
}

func (e endpoint) transport() string {
	switch {
	case e.network == "unix":
		return "unix"
	case e.useTLS:
		return "tls"
	default:
		return "tcp"
	}
}

func parseEndpoint(s string) endpoint {
	switch {
	case strings.HasPrefix(s, "unix:"):
		return endpoint{network: "unix", address: strings.TrimPrefix(s, "unix:")}
	case strings.HasPrefix(s, "tcp://"):
		return endpoint{network: "tcp", address: withPort(strings.TrimPrefix(s, "tcp://"))}
	case strings.HasPrefix(s, "tls://"):
		return endpoint{network: "tcp", address: withPort(strings.TrimPrefix(s, "tls://")), useTLS: true}
	default:
		return endpoint{network: "unix", address: s}
	}
}

func withPort(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return net.JoinHostPort(addr, defaultTCPPort)
}
