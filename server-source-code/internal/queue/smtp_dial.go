package queue

import (
	"crypto/tls"
	"net"
	"net/smtp"
	"time"
)

// implicitTLSPort reports whether a port speaks TLS from the first byte
// (SMTPS, RFC 8314). Such servers never send a plaintext greeting, so a
// plain-first dial only ever ends in EOF.
func implicitTLSPort(port int) bool {
	return port == 465
}

// dialSMTP opens an SMTP session, shared by notification and report mail.
//
//   - implicitFirst (port 465) and useTLS: TLS handshake first, then the greeting.
//   - useTLS otherwise: plain TCP, STARTTLS when offered; if the server does
//     not offer STARTTLS, reconnect with implicit TLS.
//   - !useTLS: plain TCP, never StartTLS even when advertised (local relays).
//
// The returned net.Conn must be closed by the caller after the client.
func dialSMTP(addr, host string, useTLS, implicitFirst bool, tlsCfg *tls.Config, timeout time.Duration) (*smtp.Client, net.Conn, error) {
	dialTLS := func() (*smtp.Client, net.Conn, error) {
		tlsConn, err := tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", addr, tlsCfg)
		if err != nil {
			return nil, nil, err
		}
		client, err := smtp.NewClient(tlsConn, host)
		if err != nil {
			_ = tlsConn.Close()
			return nil, nil, err
		}
		return client, tlsConn, nil
	}
	if useTLS && implicitFirst {
		return dialTLS()
	}
	plainConn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, nil, err
	}
	_ = plainConn.SetDeadline(time.Now().Add(timeout))
	client, err := smtp.NewClient(plainConn, host)
	if err != nil {
		_ = plainConn.Close()
		return nil, nil, err
	}
	_ = plainConn.SetDeadline(time.Time{})
	startTLS, _ := client.Extension("STARTTLS")
	switch {
	case useTLS && startTLS:
		if err := client.StartTLS(tlsCfg); err != nil {
			_ = client.Close()
			return nil, nil, err
		}
		return client, plainConn, nil
	case useTLS && !startTLS:
		_ = client.Close()
		return dialTLS()
	default:
		return client, plainConn, nil
	}
}
