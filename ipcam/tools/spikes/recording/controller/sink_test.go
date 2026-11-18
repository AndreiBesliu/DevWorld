package controller

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sinkServer serves a sink over TLS with a fresh self-signed certificate,
// and returns a client that trusts that certificate alone, by its DER, as
// recspike's upload does.
func sinkServer(t *testing.T) (*Sink, *httptest.Server, *http.Client) {
	t.Helper()
	cert, der, err := NewSinkCertificate()
	if err != nil {
		t.Fatal(err)
	}
	sink := NewSink()
	srv := httptest.NewUnstartedServer(sink)
	srv.TLS = SinkServerTLS(cert)
	srv.Config.ErrorLog = log.New(io.Discard, "", 0) // a refused client's handshake
	srv.StartTLS()
	t.Cleanup(srv.Close)
	tlsConfig, err := SinkClientTLS(der)
	if err != nil {
		t.Fatal(err)
	}
	return sink, srv, &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}
}

// TestSink_DiscardsAndCountsBytes (G20): the MI3 upload sink takes an HTTPS
// PUT of any size at /sink/<name>, keeps none of it, and counts its bytes
// per name; it answers with the count. Anything else is refused.
func TestSink_DiscardsAndCountsBytes(t *testing.T) {
	sink, srv, client := sinkServer(t)
	put := func(name string, body []byte) (*http.Response, error) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, srv.URL+"/sink/"+name, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		return client.Do(req)
	}
	body := bytes.Repeat([]byte("segment "), 512<<10/8*3) // 1.5 MiB
	for range 2 {
		res, err := put("upload-1", body)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Bytes int64 `json:"bytes"`
		}
		if err := json.NewDecoder(res.Body).Decode(&got); err != nil || res.StatusCode != http.StatusOK || got.Bytes != int64(len(body)) {
			t.Errorf("PUT: %d %+v %v, want 200 with bytes %d", res.StatusCode, got, err, len(body))
		}
		_ = res.Body.Close()
	}
	if got := sink.Bytes("upload-1"); got != 2*int64(len(body)) {
		t.Errorf("Bytes(upload-1) = %d, want %d", got, 2*len(body))
	}
	if got := sink.Bytes("never"); got != 0 {
		t.Errorf("Bytes(never) = %d, want 0", got)
	}

	for _, c := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/sink/upload-1", http.StatusMethodNotAllowed},
		{http.MethodPut, "/sink/", http.StatusNotFound},
		{http.MethodPut, "/sink/a/b", http.StatusNotFound},
		{http.MethodPut, "/other", http.StatusNotFound},
	} {
		req, err := http.NewRequestWithContext(t.Context(), c.method, srv.URL+c.path, strings.NewReader("x"))
		if err != nil {
			t.Fatal(err)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		if res.StatusCode != c.status {
			t.Errorf("%s %s: %d, want %d", c.method, c.path, res.StatusCode, c.status)
		}
	}
	if got := sink.Bytes("upload-1"); got != 2*int64(len(body)) {
		t.Errorf("refused requests counted: Bytes(upload-1) = %d", got)
	}
}

// TestSink_ClientTrustsOnlyThatCertificate: the upload's TLS trusts the
// sink's certificate alone, sent over the link, and no other: the sink is
// on the LAN, under no name a public CA would sign.
func TestSink_ClientTrustsOnlyThatCertificate(t *testing.T) {
	_, srv, _ := sinkServer(t)
	_, other, err := NewSinkCertificate()
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig, err := SinkClientTLS(other)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, srv.URL+"/sink/x", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Do(req)
	if err == nil {
		_ = res.Body.Close()
		t.Fatal("a client that trusts another certificate reached the sink")
	}
	var unknown x509.UnknownAuthorityError
	if !errors.As(err, &unknown) {
		t.Errorf("error %v, want x509.UnknownAuthorityError: the sink's certificate is not the one trusted", err)
	}
	if _, err := SinkClientTLS([]byte("not a certificate")); err == nil {
		t.Error("SinkClientTLS took bytes that are no certificate")
	}
}
