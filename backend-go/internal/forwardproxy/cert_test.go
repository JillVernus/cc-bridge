package forwardproxy

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
)

func TestNewCertManager_AutoGeneratesCA(t *testing.T) {
	tmpDir := t.TempDir()

	cm, err := NewCertManager(tmpDir)
	if err != nil {
		t.Fatalf("NewCertManager failed: %v", err)
	}

	// Verify CA cert file was created
	certPath := filepath.Join(tmpDir, "ca.pem")
	keyPath := filepath.Join(tmpDir, "ca-key.pem")

	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		t.Error("CA cert file was not created")
	}
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Error("CA key file was not created")
	}

	// Verify CA cert is valid
	if cm.caCert == nil {
		t.Fatal("caCert is nil")
	}
	if !cm.caCert.IsCA {
		t.Error("CA cert is not marked as CA")
	}
	if cm.caCert.Subject.CommonName != "CC-Bridge CA" {
		t.Errorf("CA CommonName = %q, want %q", cm.caCert.Subject.CommonName, "CC-Bridge CA")
	}
}

func TestNewCertManager_LoadsExistingCA(t *testing.T) {
	tmpDir := t.TempDir()

	// Create first manager (generates CA)
	cm1, err := NewCertManager(tmpDir)
	if err != nil {
		t.Fatalf("First NewCertManager failed: %v", err)
	}

	// Create second manager (should load existing CA)
	cm2, err := NewCertManager(tmpDir)
	if err != nil {
		t.Fatalf("Second NewCertManager failed: %v", err)
	}

	// Serial numbers should match (same CA loaded)
	if cm1.caCert.SerialNumber.Cmp(cm2.caCert.SerialNumber) != 0 {
		t.Error("Loaded CA has different serial number than generated CA")
	}
}

func TestCertManager_GetCertForHost(t *testing.T) {
	tmpDir := t.TempDir()
	cm, err := NewCertManager(tmpDir)
	if err != nil {
		t.Fatalf("NewCertManager failed: %v", err)
	}

	cert, err := cm.GetCertForHost("api.anthropic.com")
	if err != nil {
		t.Fatalf("GetCertForHost failed: %v", err)
	}

	if cert == nil {
		t.Fatal("cert is nil")
	}

	// Parse the generated cert
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("Failed to parse host cert: %v", err)
	}

	// Verify SAN
	found := false
	for _, name := range parsed.DNSNames {
		if name == "api.anthropic.com" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Host cert does not contain SAN for api.anthropic.com, got: %v", parsed.DNSNames)
	}

	// Verify it's signed by our CA
	roots := x509.NewCertPool()
	roots.AddCert(cm.caCert)
	if _, err := parsed.Verify(x509.VerifyOptions{Roots: roots}); err != nil {
		t.Errorf("Host cert verification failed: %v", err)
	}
}

func TestCertManager_CacheHit(t *testing.T) {
	tmpDir := t.TempDir()
	cm, err := NewCertManager(tmpDir)
	if err != nil {
		t.Fatalf("NewCertManager failed: %v", err)
	}

	cert1, err := cm.GetCertForHost("example.com")
	if err != nil {
		t.Fatalf("First GetCertForHost failed: %v", err)
	}

	cert2, err := cm.GetCertForHost("example.com")
	if err != nil {
		t.Fatalf("Second GetCertForHost failed: %v", err)
	}

	// Same pointer = cache hit
	if cert1 != cert2 {
		t.Error("Expected cache hit (same pointer), but got different certs")
	}
}

func TestCertManager_LRUEviction(t *testing.T) {
	tmpDir := t.TempDir()
	cm, err := NewCertManager(tmpDir)
	if err != nil {
		t.Fatalf("NewCertManager failed: %v", err)
	}

	// Fill cache to capacity
	for i := 0; i < lruMaxSize; i++ {
		_, err := cm.GetCertForHost(hostName(i))
		if err != nil {
			t.Fatalf("GetCertForHost(%d) failed: %v", i, err)
		}
	}

	if len(cm.cache) != lruMaxSize {
		t.Errorf("cache size = %d, want %d", len(cm.cache), lruMaxSize)
	}

	// Add one more — should evict the oldest
	_, err = cm.GetCertForHost("overflow.example.com")
	if err != nil {
		t.Fatalf("GetCertForHost(overflow) failed: %v", err)
	}

	if len(cm.cache) != lruMaxSize {
		t.Errorf("cache size after eviction = %d, want %d", len(cm.cache), lruMaxSize)
	}

	// First entry should have been evicted
	cm.mu.Lock()
	_, exists := cm.cache[hostName(0)]
	cm.mu.Unlock()
	if exists {
		t.Error("Expected first entry to be evicted, but it's still in cache")
	}
}

func TestCertManager_GetCACertPEM(t *testing.T) {
	tmpDir := t.TempDir()
	cm, err := NewCertManager(tmpDir)
	if err != nil {
		t.Fatalf("NewCertManager failed: %v", err)
	}

	pem, err := cm.GetCACertPEM()
	if err != nil {
		t.Fatalf("GetCACertPEM failed: %v", err)
	}

	if len(pem) == 0 {
		t.Error("CA cert PEM is empty")
	}

	if string(pem[:27]) != "-----BEGIN CERTIFICATE-----" {
		t.Errorf("CA cert PEM does not start with expected header, got: %q", string(pem[:27]))
	}
}

func TestCertManager_TLSHandshake(t *testing.T) {
	tmpDir := t.TempDir()
	cm, err := NewCertManager(tmpDir)
	if err != nil {
		t.Fatalf("NewCertManager failed: %v", err)
	}

	cert, err := cm.GetCertForHost("test.example.com")
	if err != nil {
		t.Fatalf("GetCertForHost failed: %v", err)
	}

	// Verify the cert can be used in a TLS config
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{*cert},
	}
	if len(tlsCfg.Certificates) != 1 {
		t.Error("TLS config has wrong number of certificates")
	}
}

func hostName(i int) string {
	return string(rune('a'+i/26)) + string(rune('a'+i%26)) + ".example.com"
}
