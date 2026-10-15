package mail

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestBuildRejectsHeaderInjection(t *testing.T) {
	if _, err := Build("a@b.c", Message{To: "x@y.z\r\nBcc: evil@e.com", Subject: "s"}, time.Now()); err == nil {
		t.Fatal("expected error")
	}
	b, err := Build("Pulse <a@b.c>", Message{To: "x@y.z", Subject: "Hi", Text: "line1\nline2"}, time.Now())
	if err != nil || !strings.Contains(string(b), "Subject: Hi\r\n") || !strings.HasSuffix(string(b), "line1\r\nline2") {
		t.Fatalf("%v %q", err, b)
	}
}

func TestNewPicksSender(t *testing.T) {
	if _, ok := New(Config{}).(LogSender); !ok {
		t.Error("empty addr should log")
	}
	if _, ok := New(Config{Addr: "h:25"}).(*SMTPSender); !ok {
		t.Error("addr should use smtp")
	}
}

// A tiny SMTP server that records one message.
func TestSMTPSend(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		r := bufio.NewReader(c)
		w := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
		w("220 test")
		var data strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if inData {
				if line == ".\r\n" {
					inData = false
					w("250 ok")
					got <- data.String()
					continue
				}
				data.WriteString(line)
				continue
			}
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				w("250 test")
			case cmd == "DATA":
				inData = true
				w("354 go")
			case cmd == "QUIT":
				w("221 bye")
				return
			default:
				w("250 ok")
			}
		}
	}()
	s := New(Config{Addr: ln.Addr().String(), From: "Pulse <no-reply@pulse.local>"})
	if err := s.Send(context.Background(), Message{To: "a@example.com", Subject: "Reset", Text: "link"}); err != nil {
		t.Fatal(err)
	}
	if msg := <-got; !strings.Contains(msg, "To: a@example.com") || !strings.Contains(msg, "link") {
		t.Errorf("message: %q", msg)
	}
}
