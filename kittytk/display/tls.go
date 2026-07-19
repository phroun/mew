package display

// Host-side TLS for tls:// endpoints. The host presents a self-signed
// certificate that clients pin (trust on first use); it must therefore
// be persistent, or a restart would change the fingerprint and trip
// every client's known_hosts. mutual TLS: the host also requires a
// client certificate, whose fingerprint drives the approval prompt.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// HostIdentityEnv overrides the path of the host identity PEM.
const HostIdentityEnv = "KITTYTK_HOST_IDENTITY"

// configDir mirrors the clients' rule so host and client share one
// per-machine config location.
func configDir() string {
	var base string
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		base = x
	} else if runtime.GOOS == "windows" {
		if a := os.Getenv("APPDATA"); a != "" {
			base = a
		}
	}
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "kittytk")
}

// fingerprintSHA256 renders a DER certificate as sha256:<hex>.
func fingerprintSHA256(der []byte) string {
	sum := sha256.Sum256(der)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// hostTLSConfig returns a mutual-TLS server config built from the given
// certificate, plus the certificate's fingerprint (what clients pin).
// The client certificate is required but not chain-verified: identity is
// established by fingerprint approval, not a CA.
func hostTLSConfig(cert tls.Certificate) (*tls.Config, string) {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAnyClientCert,
		MinVersion:   tls.VersionTLS12,
	}, fingerprintSHA256(cert.Certificate[0])
}

// LoadHostTLS builds a host TLS config from a PEM cert/key pair (for
// deployments with a provisioned certificate). Returns the config and
// its fingerprint.
func LoadHostTLS(certFile, keyFile string) (*tls.Config, string, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, "", err
	}
	cfg, fp := hostTLSConfig(cert)
	return cfg, fp, nil
}

// hostIdentityPath is $KITTYTK_HOST_IDENTITY, else
// <config>/kittytk/host_identity.pem.
func hostIdentityPath() string {
	if p := os.Getenv(HostIdentityEnv); p != "" {
		return p
	}
	return filepath.Join(configDir(), "host_identity.pem")
}

// loadOrCreateHostTLS loads the persistent host identity, generating a
// self-signed one on first use. Returns the config and its fingerprint.
func loadOrCreateHostTLS() (*tls.Config, string, error) {
	path := hostIdentityPath()
	if pemBytes, err := os.ReadFile(path); err == nil {
		cert, err := tls.X509KeyPair(pemBytes, pemBytes)
		if err != nil {
			return nil, "", err
		}
		cfg, fp := hostTLSConfig(cert)
		return cfg, fp, nil
	}
	cert, err := createHostIdentity(path)
	if err != nil {
		return nil, "", err
	}
	cfg, fp := hostTLSConfig(cert)
	return cfg, fp, nil
}

func createHostIdentity(path string) (tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "kittytk-host"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(20 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	var buf []byte
	buf = append(buf, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})...)
	buf = append(buf, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(buf, buf)
}
