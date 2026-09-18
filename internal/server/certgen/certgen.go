// Package certgen creates a self-signed TLS certificate pair for
// GophKeeper local and self-hosted deployments.
//
// The generated certificate is a self-signed leaf that acts as its
// own trust anchor: a client that adds the produced certificate file
// to its trusted roots (--tls-ca=server.crt) will successfully verify
// the server. SAN entries (DNS names and IP addresses) are supplied
// by the caller so that hostname verification succeeds for the
// address the client actually dials (e.g. localhost / 127.0.0.1).
package certgen

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Options for Generate.
type Options struct {
	// OutDir is the directory for server.crt and server.key.
	OutDir string
	// DNSNames is the list of DNS SAN entries (e.g. localhost).
	DNSNames []string
	// IPs is the list of IP SAN entries (e.g. 127.0.0.1).
	IPs []net.IP
	// Years is the certificate validity in years; defaults to 1.
	Years int
	// CommonName overrides the certificate CN; defaults to the first
	// DNS name.
	CommonName string
}

// File names written by Generate.
const (
	CertFileName = "server.crt"
	KeyFileName  = "server.key"
)

// Generate creates a self-signed certificate and private key and
// writes them to <OutDir>/server.crt and <OutDir>/server.key. It
// returns the paths of the written files.
func Generate(opts Options) (certPath, keyPath string, err error) {
	if len(opts.DNSNames) == 0 && len(opts.IPs) == 0 {
		return "", "", fmt.Errorf("certgen: at least one DNS name or IP address is required")
	}
	years := opts.Years
	if years <= 0 {
		years = 1
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("certgen: generate key: %w", err)
	}

	cn := opts.CommonName
	if cn == "" && len(opts.DNSNames) > 0 {
		cn = opts.DNSNames[0]
	}

	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn, Organization: []string{"GophKeeper"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(years, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              opts.DNSNames,
		IPAddresses:           opts.IPs,
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", fmt.Errorf("certgen: create certificate: %w", err)
	}

	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return "", "", fmt.Errorf("certgen: create output dir: %w", err)
	}

	certPath = filepath.Join(opts.OutDir, CertFileName)
	keyPath = filepath.Join(opts.OutDir, KeyFileName)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return "", "", fmt.Errorf("certgen: write certificate: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", fmt.Errorf("certgen: marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return "", "", fmt.Errorf("certgen: write key: %w", err)
	}

	return certPath, keyPath, nil
}
