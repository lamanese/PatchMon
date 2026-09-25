package reports

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func sampleMail() MailInput {
	return MailInput{
		From: "reports@example.com", To: "kunde@example.com",
		Subject: "AutoMan Report: Wöchentlicher Überblick", HTML: "<p>Grüezi – Überblick</p>",
		PDF: []byte("%PDF-1.4 fake"), PDFName: "report-ueberblick-20260926.pdf",
		Date: time.Date(2026, 9, 26, 6, 0, 0, 0, time.UTC), MessageID: "abc123",
	}
}

func TestBuildMailMessageStructure(t *testing.T) {
	raw, err := BuildMailMessage(sampleMail())
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	dec := new(mime.WordDecoder)
	subj, err := dec.DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || subj != "AutoMan Report: Wöchentlicher Überblick" {
		t.Fatalf("subject %q %v", subj, err)
	}
	if msg.Header.Get("From") != "<reports@example.com>" || msg.Header.Get("To") != "<kunde@example.com>" {
		t.Fatalf("addresses %q %q", msg.Header.Get("From"), msg.Header.Get("To"))
	}
	if msg.Header.Get("Message-ID") != "<abc123@example.com>" || msg.Header.Get("Date") == "" || msg.Header.Get("MIME-Version") != "1.0" {
		t.Fatalf("headers %v", msg.Header)
	}
	mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/mixed" {
		t.Fatalf("content type %q %v", mt, err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	p1, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if ct := p1.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("part1 %v", p1.Header)
	}
	// mime/multipart.Reader.NextPart documents that when a part's
	// Content-Transfer-Encoding is "quoted-printable" it hides that header
	// and decodes the body transparently on Read — so the header is gone
	// from p1.Header by design; check the wire format instead.
	if !bytes.Contains(raw, []byte("Content-Transfer-Encoding: quoted-printable")) {
		t.Fatal("html part must declare quoted-printable on the wire")
	}
	body, _ := io.ReadAll(p1) // multipart decodes quoted-printable transparently
	if !strings.Contains(string(body), "Grüezi – Überblick") {
		t.Fatalf("html body %q", body)
	}
	p2, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if p2.Header.Get("Content-Type") != `application/pdf; name="report-ueberblick-20260926.pdf"` || p2.Header.Get("Content-Transfer-Encoding") != "base64" {
		t.Fatalf("part2 %v", p2.Header)
	}
	_, dp, err := mime.ParseMediaType(p2.Header.Get("Content-Disposition"))
	if err != nil || dp["filename"] != "report-ueberblick-20260926.pdf" {
		t.Fatalf("disposition %v %v", dp, err)
	}
	pdf, _ := io.ReadAll(base64.NewDecoder(base64.StdEncoding, p2))
	if string(pdf) != "%PDF-1.4 fake" {
		t.Fatalf("pdf %q", pdf)
	}
	if _, err := mr.NextPart(); err != io.EOF {
		t.Fatal("exactly two parts")
	}
}

func TestBuildMailMessageEncodesNonASCIIFilename(t *testing.T) {
	in := sampleMail()
	in.PDFName = "Bericht Zürich.pdf"
	raw, err := BuildMailMessage(in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("filename*=utf-8''Bericht%20Z%C3%BCrich.pdf")) {
		t.Fatalf("RFC 2231 filename missing:\n%s", raw)
	}
}

func TestBuildMailMessageRejectsHeaderInjection(t *testing.T) {
	bad := []MailInput{
		func() MailInput { m := sampleMail(); m.To = "kunde@example.com\r\nBcc: x@y.example"; return m }(),
		func() MailInput { m := sampleMail(); m.From = "a@b.example, c@d.example"; return m }(),
		func() MailInput { m := sampleMail(); m.PDF = nil; return m }(),
	}
	for i, m := range bad {
		if _, err := BuildMailMessage(m); err == nil {
			t.Errorf("case %d must fail", i)
		}
	}
	m := sampleMail()
	m.Subject = "Report\r\nBcc: x@y.example"
	raw, err := BuildMailMessage(m)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("\r\nBcc:")) || bytes.Contains(raw, []byte("\nBcc:")) {
		t.Fatal("subject CR/LF must never start a new header line")
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	dec := new(mime.WordDecoder)
	subj, err := dec.DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || subj != "ReportBcc: x@y.example" {
		t.Fatalf("subject %q %v", subj, err)
	}
}

func TestBuildMailMessageIsDeterministic(t *testing.T) {
	a, _ := BuildMailMessage(sampleMail())
	b, _ := BuildMailMessage(sampleMail())
	if !bytes.Equal(a, b) {
		t.Fatal("same input must give identical bytes (boundary derived from content)")
	}
}
