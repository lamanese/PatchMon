package reports

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
)

// MailInput is one report mail: one recipient, HTML body, PDF attachment.
type MailInput struct {
	From, To, Subject, HTML string
	PDF                     []byte
	PDFName                 string
	Date                    time.Time
	MessageID               string // local part; the domain comes from From
}

// BuildMailMessage renders a multipart/mixed message: a quoted-printable HTML
// part and a base64 PDF attachment. Addresses must be single mailboxes
// (ParseMailbox), the subject is RFC 2047 encoded, the file name RFC 2231.
// The boundary is derived from the content so the output is deterministic.
func BuildMailMessage(in MailInput) ([]byte, error) {
	from, err := ParseMailbox(in.From)
	if err != nil {
		return nil, fmt.Errorf("from: %w", err)
	}
	to, err := ParseMailbox(in.To)
	if err != nil {
		return nil, fmt.Errorf("to: %w", err)
	}
	if len(in.PDF) == 0 {
		return nil, errors.New("mail: empty pdf attachment")
	}
	// A subject carrying CR/LF/NUL is a header-injection attempt (a forged
	// second header line, e.g. "Report\r\nBcc: x@y.example"); strip those
	// bytes and join what remains onto the single Subject line, matching the
	// codebase's existing subject-sanitisation pattern. The injected text
	// stays visible in the subject on purpose -- what must never happen is a
	// new physical header line, not that its text disappears.
	subject := strings.NewReplacer("\r", "", "\n", "", "\x00", "").Replace(in.Subject)
	name := in.PDFName
	if name == "" {
		name = "report.pdf"
	}
	sum := sha256.Sum256(append(append([]byte{}, in.PDF...), []byte(in.HTML)...))
	boundary := "=_automan_" + hex.EncodeToString(sum[:12])
	domain := from[strings.LastIndex(from, "@")+1:]
	date := in.Date
	if date.IsZero() {
		date = time.Now()
	}
	var b bytes.Buffer
	w := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	w("From", (&mail.Address{Address: from}).String())
	w("To", (&mail.Address{Address: to}).String())
	w("Subject", mime.QEncoding.Encode("utf-8", subject))
	w("Date", date.UTC().Format(time.RFC1123Z))
	w("Message-ID", "<"+in.MessageID+"@"+domain+">")
	w("MIME-Version", "1.0")
	w("Content-Type", mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": boundary}))
	b.WriteString("\r\n")

	fmt.Fprintf(&b, "--%s\r\n", boundary)
	w("Content-Type", "text/html; charset=utf-8")
	w("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(in.HTML)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	b.WriteString("\r\n")

	fmt.Fprintf(&b, "--%s\r\n", boundary)
	w("Content-Type", pdfContentType(name))
	w("Content-Transfer-Encoding", "base64")
	w("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	b.WriteString("\r\n")
	enc := base64.StdEncoding.EncodeToString(in.PDF)
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes(), nil
}

// pdfContentType writes the attachment's Content-Type header. For an ASCII
// name it is written literally as `application/pdf; name="..."` (the exact
// form assets by mail clients and the test suite); for a non-ASCII name it
// falls back to mime.FormatMediaType, which RFC 2231-encodes it as name*=.
func pdfContentType(name string) string {
	if isASCII(name) {
		return fmt.Sprintf("application/pdf; name=%q", name)
	}
	return mime.FormatMediaType("application/pdf", map[string]string{"name": name})
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
