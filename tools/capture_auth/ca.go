package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// certAuthority is a throwaway CA. It signs a leaf cert on demand for whatever
// host the phone is connecting to, so we can terminate TLS in the middle. The
// CA cert/key are persisted so the phone only has to trust one cert across runs.
type certAuthority struct {
	caCert    *x509.Certificate
	caKey     *rsa.PrivateKey
	caCertPEM []byte
	leafKey   *rsa.PrivateKey // one key reused for every leaf; keeps minting fast

	mu    sync.Mutex
	cache map[string]*tls.Certificate
}

func newCertAuthority(certPath, keyPath string) (*certAuthority, error) {
	ca := &certAuthority{cache: map[string]*tls.Certificate{}}

	if certPEM, err := os.ReadFile(certPath); err == nil {
		if keyPEM, err := os.ReadFile(keyPath); err == nil {
			if err := ca.load(certPEM, keyPEM); err != nil {
				return nil, fmt.Errorf("load CA: %w", err)
			}
		}
	}
	if ca.caCert == nil {
		if err := ca.generate(certPath, keyPath); err != nil {
			return nil, err
		}
	}

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	ca.leafKey = leafKey
	return ca, nil
}

func (ca *certAuthority) load(certPEM, keyPEM []byte) error {
	cblock, _ := pem.Decode(certPEM)
	kblock, _ := pem.Decode(keyPEM)
	if cblock == nil || kblock == nil {
		return fmt.Errorf("malformed PEM")
	}
	cert, err := x509.ParseCertificate(cblock.Bytes)
	if err != nil {
		return err
	}
	key, err := x509.ParsePKCS1PrivateKey(kblock.Bytes)
	if err != nil {
		return err
	}
	ca.caCert, ca.caKey, ca.caCertPEM = cert, key, certPEM
	return nil
}

func (ca *certAuthority) generate(certPath, keyPath string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: "owlet capture CA", Organization: []string{"owlet-go"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(5, 0, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return err
	}
	ca.caCert, ca.caKey, ca.caCertPEM = cert, key, certPEM
	return nil
}

// leafFor returns (and caches) a server cert for host, signed by the CA.
func (ca *certAuthority) leafFor(host string) (*tls.Certificate, error) {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	if c, ok := ca.cache[host]; ok {
		return c, nil
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(2, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.caCert, &ca.leafKey.PublicKey, ca.caKey)
	if err != nil {
		return nil, err
	}
	crt := &tls.Certificate{
		Certificate: [][]byte{der, ca.caCert.Raw},
		PrivateKey:  ca.leafKey,
	}
	ca.cache[host] = crt
	return crt, nil
}

// serveCert hands the CA cert to a phone browser (http://owlet.ca) so it can be
// installed and trusted.
func (ca *certAuthority) serveCert(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Header().Set("Content-Disposition", `attachment; filename="owlet-capture-ca.pem"`)
	w.Write(ca.caCertPEM)
}

func serial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	return n
}
