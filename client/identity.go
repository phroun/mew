package client

// A client's identity is a persistent self-signed certificate, generated
// once and stored like an SSH key. Its fingerprint is the stable name the
// host shows when a user approves the connection (Always / Never act on
// it), so it must survive restarts. Generation is lazy and cached.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// IdentityEnv overrides the path of the client identity PEM (key + cert).
const IdentityEnv = "KITTYTK_IDENTITY"

var (
	identityOnce sync.Once
	identityCert tls.Certificate
	identityErr  error
)

// identityPath is $KITTYTK_IDENTITY, else <config>/kittytk/identity.pem.
func identityPath() string {
	if p := os.Getenv(IdentityEnv); p != "" {
		return p
	}
	return filepath.Join(ConfigDir(), "identity.pem")
}

// clientIdentity loads (or, on first use, generates and stores) this
// user's identity certificate. The result is cached for the process.
func clientIdentity() (tls.Certificate, error) {
	identityOnce.Do(func() {
		identityCert, identityErr = loadOrCreateIdentity(identityPath())
	})
	return identityCert, identityErr
}

// IdentityFingerprint returns sha256:<hex> of the client identity cert,
// the value a host displays for approval. Generates the identity if
// absent.
func IdentityFingerprint() (string, error) {
	cert, err := clientIdentity()
	if err != nil {
		return "", err
	}
	return FingerprintSHA256(cert.Certificate[0]), nil
}

func loadOrCreateIdentity(path string) (tls.Certificate, error) {
	if pemBytes, err := os.ReadFile(path); err == nil {
		return tls.X509KeyPair(pemBytes, pemBytes)
	}
	return createIdentity(path)
}

func createIdentity(path string) (tls.Certificate, error) {
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
		Subject:               pkix.Name{CommonName: "kittytk-client"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(20 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
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
	// 0600: the private key lives here.
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		return tls.Certificate{}, fmt.Errorf("write identity: %w", err)
	}
	return tls.X509KeyPair(buf, buf)
}
