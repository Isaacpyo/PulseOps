// Package probe executes HTTP and TCP health probes against check targets.
package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Kind is the probe protocol.
type Kind string

const (
	KindHTTP Kind = "http"
	KindTCP  Kind = "tcp"
)

// Spec describes a single probe to run.
type Spec struct {
	Kind    Kind
	Target  string // URL for http, host:port for tcp
	Timeout time.Duration
	// ExpectedStatus is the required HTTP status; 0 accepts any 2xx/3xx.
	ExpectedStatus int
}

// Result is the outcome of one probe.
type Result struct {
	OK         bool
	Latency    time.Duration
	StatusCode int
	Error      string
}

// UserAgent identifies PulseOps probes to target servers.
const UserAgent = "PulseOps-Probe/1.0 (+https://pulseops.temitopeagbola.com)"

const maxBodyBytes = 64 << 10

// Prober runs probes. It is safe for concurrent use.
type Prober struct {
	allowPrivate bool
	dialer       *net.Dialer
	transport    *http.Transport
}

// New builds a Prober. When allowPrivate is false every outbound connection
// is checked against the SSRF guard after DNS resolution.
func New(allowPrivate bool) *Prober {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: -1,
		Control:   guardControl(allowPrivate),
	}
	transport := &http.Transport{
		// Probes must hit the target directly, never via an env-configured proxy.
		Proxy:                  nil,
		DialContext:            dialer.DialContext,
		DisableKeepAlives:      true, // measure a full connect every time
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  30 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
		ForceAttemptHTTP2:      true,
	}
	return &Prober{allowPrivate: allowPrivate, dialer: dialer, transport: transport}
}

// Run executes the probe described by spec.
func (p *Prober) Run(ctx context.Context, spec Spec) Result {
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch spec.Kind {
	case KindHTTP:
		return p.runHTTP(ctx, spec, timeout)
	case KindTCP:
		return p.runTCP(ctx, spec, timeout)
	default:
		return Result{Error: fmt.Sprintf("unsupported probe kind %q", spec.Kind)}
	}
}

func (p *Prober) runHTTP(ctx context.Context, spec Spec, timeout time.Duration) Result {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.Target, nil)
	if err != nil {
		return Result{Error: "invalid URL"}
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Cache-Control", "no-cache")

	client := &http.Client{
		Transport: p.transport,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("stopped after 5 redirects")
			}
			return nil
		},
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return Result{Latency: latency, Error: describeError(err, timeout)}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyBytes))

	res := Result{Latency: latency, StatusCode: resp.StatusCode}
	switch {
	case spec.ExpectedStatus > 0 && resp.StatusCode != spec.ExpectedStatus:
		res.Error = fmt.Sprintf("expected HTTP %d, got %d", spec.ExpectedStatus, resp.StatusCode)
	case spec.ExpectedStatus == 0 && (resp.StatusCode < 200 || resp.StatusCode >= 400):
		res.Error = fmt.Sprintf("unexpected HTTP %d", resp.StatusCode)
	default:
		res.OK = true
	}
	return res
}

func (p *Prober) runTCP(ctx context.Context, spec Spec, timeout time.Duration) Result {
	start := time.Now()
	conn, err := p.dialer.DialContext(ctx, "tcp", spec.Target)
	latency := time.Since(start)
	if err != nil {
		return Result{Latency: latency, Error: describeError(err, timeout)}
	}
	_ = conn.Close()
	return Result{OK: true, Latency: latency}
}

// describeError turns low-level network errors into short, operator-friendly
// messages suitable for timelines and Slack alerts.
func describeError(err error, timeout time.Duration) string {
	var (
		dnsErr  *net.DNSError
		certErr *tls.CertificateVerificationError
		unkAuth x509.UnknownAuthorityError
		hostErr x509.HostnameError
		invErr  x509.CertificateInvalidError
		netErr  net.Error
	)
	switch {
	case errors.Is(err, ErrBlockedAddress):
		return "blocked: " + ErrBlockedAddress.Error()
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded):
		return "timed out after " + timeout.String()
	case errors.As(err, &dnsErr):
		if dnsErr.IsNotFound {
			return "DNS: no such host " + dnsErr.Name
		}
		return "DNS lookup failed for " + dnsErr.Name
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection reset by peer"
	case errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETUNREACH):
		return "host unreachable"
	case errors.As(err, &hostErr):
		return "TLS: certificate is not valid for " + hostErr.Host
	case errors.As(err, &unkAuth):
		return "TLS: certificate signed by unknown authority"
	case errors.As(err, &invErr):
		if invErr.Reason == x509.Expired {
			return "TLS: certificate has expired or is not yet valid"
		}
		return "TLS: invalid certificate"
	case errors.As(err, &certErr):
		return "TLS: certificate verification failed"
	case errors.As(err, &netErr) && netErr.Timeout():
		return "timed out after " + timeout.String()
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}

// ValidateTarget checks a user-supplied target for the given kind and
// returns it normalised. It is the API-boundary validation; runtime SSRF
// enforcement happens in the dialer.
func ValidateTarget(kind Kind, target string, allowPrivate bool) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("is required")
	}
	if len(target) > 2048 {
		return "", errors.New("must be at most 2048 characters")
	}
	switch kind {
	case KindHTTP:
		u, err := url.Parse(target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "", errors.New("must be an absolute http:// or https:// URL")
		}
		if u.User != nil {
			return "", errors.New("must not contain credentials")
		}
		if port := u.Port(); port != "" {
			if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
				return "", errors.New("has an invalid port")
			}
		}
		if err := CheckHostLiteral(u.Hostname(), allowPrivate); err != nil {
			return "", errors.New("must be a public address (private, loopback and link-local targets are blocked)")
		}
		u.Fragment = ""
		return u.String(), nil
	case KindTCP:
		host, port, err := net.SplitHostPort(target)
		if err != nil || host == "" {
			return "", errors.New("must be host:port")
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", errors.New("port must be between 1 and 65535")
		}
		if err := CheckHostLiteral(host, allowPrivate); err != nil {
			return "", errors.New("must be a public address (private, loopback and link-local targets are blocked)")
		}
		return net.JoinHostPort(host, port), nil
	default:
		return "", fmt.Errorf("unsupported kind %q", kind)
	}
}
