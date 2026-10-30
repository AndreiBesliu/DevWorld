// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package controller

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SinkServerName is the name in the sink's certificate, which the upload's
// TLS checks instead of an address: the host reaches the controller by
// whatever address it has on the LAN.
const SinkServerName = "reccontroller"

// sinkPrefix is the sink's path: PUT /sink/<name>.
const sinkPrefix = "/sink/"

// Sink is MI3's upload sink (G20): an HTTPS endpoint of reccontroller that
// takes a PUT of any size at /sink/<name>, keeps none of it and counts its
// bytes per name, so that the host's encrypted upload costs nothing and
// needs no account (O13). It answers each PUT with {"bytes": <n>}, the
// bytes of that request.
type Sink struct {
	mu    sync.Mutex
	bytes map[string]int64
}

// NewSink returns a sink that has counted nothing.
func NewSink() *Sink { return &Sink{bytes: map[string]int64{}} }

// Bytes is what the PUTs to name brought, in all.
func (s *Sink) Bytes(name string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bytes[name]
}

// ServeHTTP discards a PUT's body and counts it.
func (s *Sink) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name, ok := strings.CutPrefix(r.URL.Path, sinkPrefix)
	if !ok || name == "" || strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", http.MethodPut)
		http.Error(w, "PUT only", http.StatusMethodNotAllowed)
		return
	}
	n, err := io.Copy(counter{s: s, name: name}, r.Body)
	if err != nil {
		http.Error(w, "the body did not arrive", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Bytes int64 `json:"bytes"`
	}{n})
}

// counter counts the bytes written to it under its name, as they come.
type counter struct {
	s    *Sink
	name string
}

func (c counter) Write(p []byte) (int, error) {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	c.s.bytes[c.name] += int64(len(p))
	return len(p), nil
}

// sinkCertValidity is how long the sink's certificate is valid: one
// reccontroller start makes its own.
const sinkCertValidity = 30 * 24 * time.Hour

// NewSinkCertificate makes the sink's self-signed certificate for
// SinkServerName, with a fresh P-256 key, and returns it with its DER,
// which the host gets over the link and trusts alone (SinkClientTLS).
func NewSinkCertificate() (tls.Certificate, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: SinkServerName}, DNSNames: []string{SinkServerName},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(sinkCertValidity),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, der, nil
}

// SinkClientTLS is the upload's TLS configuration: it trusts the sink's
// certificate der alone, for SinkServerName, with TLS 1.3. The check stays
// on: a LAN peer that is not this reccontroller cannot take the upload.
func SinkClientTLS(der []byte) (*tls.Config, error) {
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("controller: the sink's certificate: %w", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &tls.Config{RootCAs: pool, ServerName: SinkServerName, MinVersion: tls.VersionTLS13}, nil
}

// SinkServerTLS is the sink's TLS configuration with cert.
func SinkServerTLS(cert tls.Certificate) *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
}
