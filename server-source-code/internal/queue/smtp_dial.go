package queue

import (
	"context"
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
// timeout bounds the dial and the greeting; the connection deadline is
// cleared afterwards. The returned net.Conn must be closed by the caller
// after the client.
func dialSMTP(addr, host string, useTLS, implicitFirst bool, tlsCfg *tls.Config, timeout time.Duration) (*smtp.Client, net.Conn, error) {
	return dialSMTPUntil(context.Background(), addr, host, useTLS, implicitFirst, tlsCfg, timeout, time.Time{})
}

// dialSMTPUntil is dialSMTP with a context and an absolute deadline. A
// non-zero deadline is set on the connection BEFORE the greeting is read and
// stays set (STARTTLS included), so a server that accepts and then stays
// silent cannot hold the session past it. A zero deadline keeps dialSMTP's
// behaviour (timeout for dial and greeting, then cleared).
func dialSMTPUntil(ctx context.Context, addr, host string, useTLS, implicitFirst bool, tlsCfg *tls.Config, timeout time.Duration, deadline time.Time) (*smtp.Client, net.Conn, error) {
	dialer := &net.Dialer{Timeout: timeout, Deadline: deadline}
	greetingDeadline := deadline
	if greetingDeadline.IsZero() {
		greetingDeadline = time.Now().Add(timeout)
	}
	// greet reads the server greeting under the deadline.
	greet := func(conn net.Conn) (*smtp.Client, error) {
		_ = conn.SetDeadline(greetingDeadline)
		client, err := smtp.NewClient(conn, host)
		if err != nil {
			return nil, err
		}
		if deadline.IsZero() {
			_ = conn.SetDeadline(time.Time{})
		}
		return client, nil
	}
	dialTLS := func() (*smtp.Client, net.Conn, error) {
		tlsConn, err := (&tls.Dialer{NetDialer: dialer, Config: tlsCfg}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, nil, err
		}
		client, err := greet(tlsConn)
		if err != nil {
			_ = tlsConn.Close()
			return nil, nil, err
		}
		return client, tlsConn, nil
	}
	if useTLS && implicitFirst {
		return dialTLS()
	}
	plainConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	client, err := greet(plainConn)
	if err != nil {
		_ = plainConn.Close()
		return nil, nil, err
	}
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
