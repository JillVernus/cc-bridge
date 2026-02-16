package forwardproxy

import (
	"crypto/rand"
	"crypto/rsa"
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

const (
	caCertFile = "ca.pem"
	caKeyFile  = "ca-key.pem"
	lruMaxSize = 100
)

// CertManager handles CA certificate generation and per-host TLS certificate signing.
type CertManager struct {
	certDir string
	caCert  *x509.Certificate
	caKey   *rsa.PrivateKey

	mu    sync.Mutex
	cache map[string]*lruEntry
	order []string // oldest first
}

type lruEntry struct {
	cert *tls.Certificate
}

// NewCertManager loads or auto-generates a CA certificate in certDir.
func NewCertManager(certDir string) (*CertManager, error) {
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return nil, fmt.Errorf("create cert dir: %w", err)
	}

	cm := &CertManager{
		certDir: certDir,
		cache:   make(map[string]*lruEntry),
	}

	certPath := filepath.Join(certDir, caCertFile)
	keyPath := filepath.Join(certDir, caKeyFile)

	// Try loading existing CA
	if err := cm.loadCA(certPath, keyPath); err != nil {
		// Generate new CA
		if err := cm.generateCA(certPath, keyPath); err != nil {
			return nil, fmt.Errorf("generate CA: %w", err)
		}
	}

	return cm, nil
}

// GetCACertPEM returns the CA certificate in PEM format for download.
func (cm *CertManager) GetCACertPEM() ([]byte, error) {
	certPath := filepath.Join(cm.certDir, caCertFile)
	return os.ReadFile(certPath)
}

// GetCertForHost returns a TLS certificate for the given host, signed by the CA.
// Results are cached with LRU eviction.
func (cm *CertManager) GetCertForHost(host string) (*tls.Certificate, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if entry, ok := cm.cache[host]; ok {
		// Move to end (most recently used)
		cm.moveToEnd(host)
		return entry.cert, nil
	}

	cert, err := cm.generateHostCert(host)
	if err != nil {
		return nil, err
	}

	// Evict oldest if at capacity
	if len(cm.order) >= lruMaxSize {
		oldest := cm.order[0]
		cm.order = cm.order[1:]
		delete(cm.cache, oldest)
	}

	cm.cache[host] = &lruEntry{cert: cert}
	cm.order = append(cm.order, host)
	return cert, nil
}

func (cm *CertManager) moveToEnd(host string) {
	for i, h := range cm.order {
		if h == host {
			cm.order = append(cm.order[:i], cm.order[i+1:]...)
			cm.order = append(cm.order, host)
			return
		}
	}
}

func (cm *CertManager) loadCA(certPath, keyPath string) error {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return fmt.Errorf("failed to decode CA cert PEM")
	}
	caCert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return fmt.Errorf("parse CA cert: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return fmt.Errorf("failed to decode CA key PEM")
	}
	caKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return fmt.Errorf("parse CA key: %w", err)
	}

	cm.caCert = caCert
	cm.caKey = caKey
	return nil
}

func (cm *CertManager) generateCA(certPath, keyPath string) error {
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate CA key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("generate serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"CC-Bridge Forward Proxy"},
			CommonName:   "CC-Bridge CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour), // 10 years
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &caKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create CA cert: %w", err)
	}

	caCert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return fmt.Errorf("parse generated CA cert: %w", err)
	}

	// Write cert PEM
	certFile, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("create cert file: %w", err)
	}
	defer certFile.Close()
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return fmt.Errorf("write cert PEM: %w", err)
	}

	// Write key PEM (restrictive permissions)
	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create key file: %w", err)
	}
	defer keyFile.Close()
	if err := pem.Encode(keyFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caKey)}); err != nil {
		return fmt.Errorf("write key PEM: %w", err)
	}

	cm.caCert = caCert
	cm.caKey = caKey
	return nil
}

func (cm *CertManager) generateHostCert(host string) (*tls.Certificate, error) {
	hostKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate host key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"CC-Bridge Forward Proxy"},
			CommonName:   host,
		},
		DNSNames:  []string{host},
		NotBefore: time.Now().Add(-1 * time.Hour),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour), // 1 year
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, cm.caCert, &hostKey.PublicKey, cm.caKey)
	if err != nil {
		return nil, fmt.Errorf("create host cert: %w", err)
	}

	tlsCert := &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  hostKey,
	}
	return tlsCert, nil
}
