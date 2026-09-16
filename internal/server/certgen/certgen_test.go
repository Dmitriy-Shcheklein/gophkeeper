package certgen_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmitriy/gophkeeper/internal/server/certgen"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateCreatesValidPair(t *testing.T) {
	outDir := t.TempDir()

	certPath, keyPath, err := certgen.Generate(certgen.Options{
		OutDir:   outDir,
		DNSNames: []string{"localhost"},
		IPs:      []net.IP{net.ParseIP("127.0.0.1")},
	})
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(outDir, "server.crt"), certPath)
	assert.Equal(t, filepath.Join(outDir, "server.key"), keyPath)

	certPEM, err := os.ReadFile(certPath)
	require.NoError(t, err)
	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block, "no CERTIFICATE block in server.crt")

	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	assert.Equal(t, []string{"localhost"}, cert.DNSNames)
	assert.True(t, cert.IPAddresses[0].Equal(net.ParseIP("127.0.0.1")), "IP SAN must be 127.0.0.1")
	assert.Equal(t, x509.ECDSAWithSHA256, cert.SignatureAlgorithm)
	assert.WithinDuration(t, time.Now().AddDate(1, 0, 0), cert.NotAfter, 24*time.Hour)
	assert.WithinDuration(t, time.Now().Add(-2*time.Hour), cert.NotBefore, 24*time.Hour)

	keyPEM, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	keyBlock, _ := pem.Decode(keyPEM)
	require.NotNil(t, keyBlock, "no private key block in server.key")
	_, err = x509.ParseECPrivateKey(keyBlock.Bytes)
	require.NoError(t, err)

	// The pair must load as a working TLS certificate.
	_, err = tls.LoadX509KeyPair(certPath, keyPath)
	require.NoError(t, err)

	info, err := os.Stat(keyPath)
	require.NoError(t, err)
	assert.Zero(t, info.Mode().Perm()&0o077, "private key must not be group/world readable")
}

func TestGenerateCustomYearsAndCN(t *testing.T) {
	outDir := t.TempDir()

	certPath, _, err := certgen.Generate(certgen.Options{
		OutDir:     outDir,
		DNSNames:   []string{"gk.example.com"},
		CommonName: "custom-cn",
		Years:      3,
	})
	require.NoError(t, err)

	certPEM, err := os.ReadFile(certPath)
	require.NoError(t, err)
	block, _ := pem.Decode(certPEM)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	assert.Equal(t, "custom-cn", cert.Subject.CommonName)
	assert.WithinDuration(t, time.Now().AddDate(3, 0, 0), cert.NotAfter, 48*time.Hour)
}

func TestGenerateRequiresSAN(t *testing.T) {
	_, _, err := certgen.Generate(certgen.Options{OutDir: t.TempDir()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one DNS name or IP address")
}

func TestGenerateTLSCanonicalVerification(t *testing.T) {
	// The self-signed certificate must verify as its own trust anchor
	// with hostname verification against its SAN entries.
	outDir := t.TempDir()
	certPath, _, err := certgen.Generate(certgen.Options{
		OutDir:   outDir,
		DNSNames: []string{"localhost"},
		IPs:      []net.IP{net.ParseIP("127.0.0.1")},
	})
	require.NoError(t, err)

	certPEM, err := os.ReadFile(certPath)
	require.NoError(t, err)
	block, _ := pem.Decode(certPEM)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	pool.AddCert(cert)

	_, err = cert.Verify(x509.VerifyOptions{
		Roots:     pool,
		DNSName:   "localhost",
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		CurrentTime: func() time.Time { // cert.NotBefore is backdated 1h
			return time.Now()
		}(),
	})
	require.NoError(t, err)
}
