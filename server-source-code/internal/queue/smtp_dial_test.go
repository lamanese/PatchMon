package queue

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// tlsOnlySMTPServer behaves like port 465 on a real mail server: it expects a
// TLS handshake first and never sends a plaintext greeting.
func tlsOnlySMTPServer(t *testing.T) (addr string, tlsCfg *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"localhost"}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				if _, err := c.Write([]byte("220 localhost ESMTP ready\r\n")); err != nil {
					return // handshake failed: a plaintext client never gets a greeting
				}
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						_, _ = c.Write([]byte("250-localhost\r\n250 AUTH PLAIN\r\n"))
					case strings.HasPrefix(line, "QUIT"):
						_, _ = c.Write([]byte("221 bye\r\n"))
						return
					default:
						_, _ = c.Write([]byte("250 ok\r\n"))
					}
				}
			}(conn)
		}
	}()
	pool := x509.NewCertPool()
	leaf, _ := x509.ParseCertificate(der)
	pool.AddCert(leaf)
	return ln.Addr().String(), &tls.Config{ServerName: "localhost", RootCAs: pool, MinVersion: tls.VersionTLS12}
}

func TestDialSMTPImplicitTLSFirstReachesATLSOnlyServer(t *testing.T) {
	addr, tlsCfg := tlsOnlySMTPServer(t)
	c, conn, err := dialSMTP(addr, "localhost", true, true, tlsCfg, 3*time.Second)
	if err != nil {
		t.Fatalf("implicit TLS first must succeed against a 465-style server: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, ok := conn.(*tls.Conn); !ok {
		t.Fatal("connection must be TLS")
	}
	if ok, _ := c.Extension("AUTH"); !ok {
		t.Fatal("EHLO over the TLS connection must have been read")
	}
	_ = c.Quit()
}

func TestDialSMTPPlainFirstFailsOnATLSOnlyServer(t *testing.T) {
	addr, tlsCfg := tlsOnlySMTPServer(t)
	_, _, err := dialSMTP(addr, "localhost", true, false, tlsCfg, 2*time.Second)
	if err == nil {
		t.Fatal("plain-first against a TLS-only server is the bug being fixed; it must not silently succeed here")
	}
}

func TestImplicitTLSPort(t *testing.T) {
	if !implicitTLSPort(465) || implicitTLSPort(587) || implicitTLSPort(25) || implicitTLSPort(0) {
		t.Fatal("only 465 means implicit TLS")
	}
}
