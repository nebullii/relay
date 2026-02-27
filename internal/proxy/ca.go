package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"sync"
	"time"
)

// CA holds the local certificate authority used for MITM interception.
type CA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	tlsCert tls.Certificate

	mu    sync.Mutex
	cache map[string]*tls.Certificate
}

// LoadOrCreateCA loads an existing CA from disk or generates a new one.
func LoadOrCreateCA(certPath, keyPath string) (*CA, error) {
	certData, certErr := os.ReadFile(certPath)
	keyData, keyErr := os.ReadFile(keyPath)
	if certErr == nil && keyErr == nil {
		tlsCert, err := tls.X509KeyPair(certData, keyData)
		if err == nil {
			x509Cert, err := x509.ParseCertificate(tlsCert.Certificate[0])
			if err == nil && time.Now().Before(x509Cert.NotAfter) {
				return &CA{cert: x509Cert, tlsCert: tlsCert, cache: make(map[string]*tls.Certificate)}, nil
			}
		}
	}
	return createCA(certPath, keyPath)
}

func createCA(certPath, keyPath string) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "relay local CA",
			Organization: []string{"relay"},
		},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	x509Cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, err
	}

	cf, err := os.Create(certPath)
	if err != nil {
		return nil, err
	}
	pem.Encode(cf, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	cf.Close()

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	kf, err := os.Create(keyPath)
	if err != nil {
		return nil, err
	}
	pem.Encode(kf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	kf.Close()
	os.Chmod(keyPath, 0600)

	tlsCert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
		Leaf:        x509Cert,
	}
	return &CA{cert: x509Cert, key: key, tlsCert: tlsCert, cache: make(map[string]*tls.Certificate)}, nil
}

// CertForHost returns a TLS certificate for the given hostname, signed by the CA.
// Certificates are cached in memory.
func (ca *CA) CertForHost(host string) (*tls.Certificate, error) {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	if c, ok := ca.cache[host]; ok {
		return c, nil
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-24 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		return nil, err
	}

	cert := &tls.Certificate{Certificate: [][]byte{certDER}, PrivateKey: key}
	ca.cache[host] = cert
	return cert, nil
}

// CertPath returns the PEM-encoded CA certificate bytes for display/export.
func (ca *CA) CertPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.cert.Raw})
}
