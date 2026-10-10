package probe

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestIsPublicAddr(t *testing.T) {
	cases := map[string]bool{
		"1.1.1.1":          true,
		"8.8.8.8":          true,
		"2606:4700::1111":  true,
		"127.0.0.1":        false,
		"10.1.2.3":         false,
		"172.16.0.1":       false,
		"192.168.1.1":      false,
		"169.254.169.254":  false, // cloud metadata
		"100.64.0.1":       false,
		"0.0.0.0":          false,
		"::1":              false,
		"fe80::1":          false,
		"fd00:ec2::254":    false,
		"::ffff:127.0.0.1": false, // IPv4-mapped loopback
		"224.0.0.1":        false,
		"64:ff9b::a00:1":   false,
	}
	for in, want := range cases {
		if got := IsPublicAddr(netip.MustParseAddr(in)); got != want {
			t.Errorf("IsPublicAddr(%s) = %v, want %v", in, got, want)
		}
	}
}

func TestValidateTarget(t *testing.T) {
	cases := []struct {
		kind    Kind
		in      string
		private bool
		want    string
		wantErr string
	}{
		{KindHTTP, "https://example.com/health", false, "https://example.com/health", ""},
		{KindHTTP, "  https://example.com/#frag ", false, "https://example.com/", ""},
		{KindHTTP, "ftp://example.com", false, "", "http://"},
		{KindHTTP, "example.com", false, "", "http://"},
		{KindHTTP, "https://user:pass@example.com", false, "", "credentials"},
		{KindHTTP, "http://127.0.0.1:8080", false, "", "public address"},
		{KindHTTP, "http://localhost/", false, "", "public address"},
		{KindHTTP, "http://[::1]/", false, "", "public address"},
		{KindHTTP, "http://169.254.169.254/latest/meta-data", false, "", "public address"},
		{KindHTTP, "http://localhost:8080/healthz", true, "http://localhost:8080/healthz", ""},
		{KindHTTP, "https://example.com:99999", false, "", "port"},
		{KindTCP, "1.1.1.1:53", false, "1.1.1.1:53", ""},
		{KindTCP, "db.example.com:5432", false, "db.example.com:5432", ""},
		{KindTCP, "example.com", false, "", "host:port"},
		{KindTCP, "example.com:0", false, "", "port"},
		{KindTCP, "10.0.0.5:5432", false, "", "public address"},
		{KindTCP, "", false, "", "required"},
		{"icmp", "1.1.1.1", false, "", "unsupported"},
	}
	for _, tc := range cases {
		got, err := ValidateTarget(tc.kind, tc.in, tc.private)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("ValidateTarget(%s, %q) err = %v, want containing %q", tc.kind, tc.in, err, tc.wantErr)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ValidateTarget(%s, %q) = %q, %v; want %q", tc.kind, tc.in, got, err, tc.want)
		}
	}
}

func TestHTTPProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != UserAgent {
			t.Errorf("missing probe user agent")
		}
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
		case "/redirect":
			http.Redirect(w, r, "/ok", http.StatusFound)
		case "/teapot":
			w.WriteHeader(http.StatusTeapot)
		case "/slow":
			time.Sleep(300 * time.Millisecond)
		default:
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	p := New(true)
	ctx := context.Background()

	if r := p.Run(ctx, Spec{Kind: KindHTTP, Target: srv.URL + "/ok", Timeout: time.Second}); !r.OK || r.StatusCode != 200 || r.Latency <= 0 {
		t.Fatalf("ok probe: %+v", r)
	}
	if r := p.Run(ctx, Spec{Kind: KindHTTP, Target: srv.URL + "/redirect", Timeout: time.Second}); !r.OK || r.StatusCode != 200 {
		t.Fatalf("redirects should be followed: %+v", r)
	}
	if r := p.Run(ctx, Spec{Kind: KindHTTP, Target: srv.URL + "/err", Timeout: time.Second}); r.OK || r.StatusCode != 500 || r.Error != "unexpected HTTP 500" {
		t.Fatalf("500 should fail: %+v", r)
	}
	if r := p.Run(ctx, Spec{Kind: KindHTTP, Target: srv.URL + "/teapot", Timeout: time.Second, ExpectedStatus: 418}); !r.OK {
		t.Fatalf("expected status should pass: %+v", r)
	}
	if r := p.Run(ctx, Spec{Kind: KindHTTP, Target: srv.URL + "/ok", Timeout: time.Second, ExpectedStatus: 204}); r.OK || !strings.Contains(r.Error, "expected HTTP 204") {
		t.Fatalf("mismatched status should fail: %+v", r)
	}
	if r := p.Run(ctx, Spec{Kind: KindHTTP, Target: srv.URL + "/slow", Timeout: 100 * time.Millisecond}); r.OK || !strings.Contains(r.Error, "timed out") {
		t.Fatalf("slow probe should time out: %+v", r)
	}
}

func TestProbeBlocksPrivateTargetsAtDialTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	p := New(false)
	r := p.Run(context.Background(), Spec{Kind: KindHTTP, Target: srv.URL, Timeout: time.Second})
	if r.OK || !strings.HasPrefix(r.Error, "blocked:") {
		t.Fatalf("loopback HTTP target must be blocked, got %+v", r)
	}
	r = p.Run(context.Background(), Spec{Kind: KindTCP, Target: srv.Listener.Addr().String(), Timeout: time.Second})
	if r.OK || !strings.HasPrefix(r.Error, "blocked:") {
		t.Fatalf("loopback TCP target must be blocked, got %+v", r)
	}
}

func TestTCPProbe(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	p := New(true)
	if r := p.Run(context.Background(), Spec{Kind: KindTCP, Target: addr, Timeout: time.Second}); !r.OK {
		t.Fatalf("tcp probe should succeed: %+v", r)
	}
	ln.Close()
	if r := p.Run(context.Background(), Spec{Kind: KindTCP, Target: addr, Timeout: time.Second}); r.OK || r.Error != "connection refused" {
		t.Fatalf("closed port should be refused: %+v", r)
	}
}

func TestUnsupportedKind(t *testing.T) {
	r := New(true).Run(context.Background(), Spec{Kind: "icmp", Target: "x"})
	if r.OK || !strings.Contains(r.Error, "unsupported") {
		t.Fatalf("got %+v", r)
	}
}
