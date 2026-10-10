package metrics

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/encoding/protowire"
)

// Label is a Prometheus label pair.
type Label struct{ Name, Value string }

// Sample is a single value at a millisecond timestamp.
type Sample struct {
	Value     float64
	Timestamp int64
}

// TimeSeries is one remote_write series.
type TimeSeries struct {
	Labels  []Label
	Samples []Sample
}

// RemoteWriter periodically gathers the registry and pushes it using the
// Prometheus remote_write 1.0 protocol (snappy-compressed protobuf). This
// lets the API ship metrics to Grafana Cloud without running a Prometheus
// or Alloy sidecar, which a free Render web service cannot host.
type RemoteWriter struct {
	URL       string
	Username  string
	Password  string
	Interval  time.Duration
	Gatherer  prometheus.Gatherer
	Labels    map[string]string // added to every series (job, instance)
	Client    *http.Client
	Pushes    prometheus.Counter
	Failures  prometheus.Counter
	userAgent string
}

// NewRemoteWriter builds a RemoteWriter and registers its own self-metrics.
func NewRemoteWriter(m *Metrics, url, username, password string, interval time.Duration, labels map[string]string) *RemoteWriter {
	rw := &RemoteWriter{
		URL: url, Username: username, Password: password, Interval: interval,
		Gatherer: m.Registry, Labels: labels,
		Client: &http.Client{Timeout: 15 * time.Second},
		Pushes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "pulseops_remote_write_pushes_total", Help: "Successful remote_write pushes.",
		}),
		Failures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "pulseops_remote_write_failures_total", Help: "Failed remote_write pushes.",
		}),
		userAgent: "pulseops-remote-write/1.0",
	}
	m.Registry.MustRegister(rw.Pushes, rw.Failures)
	return rw
}

// Run pushes on every interval until ctx is cancelled.
func (rw *RemoteWriter) Run(ctx context.Context) {
	t := time.NewTicker(rw.Interval)
	defer t.Stop()
	slog.Info("remote_write enabled", "url", rw.URL, "interval", rw.Interval)
	for {
		select {
		case <-ctx.Done():
			// Best-effort final flush so the last interval is not lost on shutdown.
			flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = rw.PushOnce(flushCtx)
			cancel()
			return
		case <-t.C:
			if err := rw.PushOnce(ctx); err != nil {
				slog.Warn("remote_write push failed", "err", err)
			}
		}
	}
}

// PushOnce gathers and pushes a single batch.
func (rw *RemoteWriter) PushOnce(ctx context.Context) error {
	mfs, err := rw.Gatherer.Gather()
	if err != nil {
		rw.Failures.Inc()
		return fmt.Errorf("gather: %w", err)
	}
	series := ToTimeSeries(mfs, time.Now(), rw.Labels)
	body := snappy.Encode(nil, EncodeWriteRequest(series))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rw.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	req.Header.Set("User-Agent", rw.userAgent)
	if rw.Username != "" || rw.Password != "" {
		req.SetBasicAuth(rw.Username, rw.Password)
	}
	resp, err := rw.Client.Do(req)
	if err != nil {
		rw.Failures.Inc()
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		rw.Failures.Inc()
		return fmt.Errorf("remote_write returned %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}
	rw.Pushes.Inc()
	return nil
}

// ToTimeSeries flattens gathered metric families into remote_write series,
// expanding histograms and summaries the same way the text exposition does.
func ToTimeSeries(mfs []*dto.MetricFamily, now time.Time, extra map[string]string) []TimeSeries {
	ts := now.UnixMilli()
	var out []TimeSeries
	add := func(name string, base []*dto.LabelPair, value float64, more ...Label) {
		labels := make([]Label, 0, len(base)+len(extra)+len(more)+1)
		labels = append(labels, Label{"__name__", name})
		seen := map[string]bool{"__name__": true}
		for _, lp := range base {
			labels = append(labels, Label{lp.GetName(), lp.GetValue()})
			seen[lp.GetName()] = true
		}
		labels = append(labels, more...)
		for _, l := range more {
			seen[l.Name] = true
		}
		for k, v := range extra {
			if !seen[k] {
				labels = append(labels, Label{k, v})
			}
		}
		sort.Slice(labels, func(i, j int) bool { return labels[i].Name < labels[j].Name })
		out = append(out, TimeSeries{Labels: labels, Samples: []Sample{{Value: value, Timestamp: ts}}})
	}

	for _, mf := range mfs {
		name := mf.GetName()
		for _, m := range mf.GetMetric() {
			lp := m.GetLabel()
			switch mf.GetType() {
			case dto.MetricType_COUNTER:
				add(name, lp, m.GetCounter().GetValue())
			case dto.MetricType_GAUGE:
				add(name, lp, m.GetGauge().GetValue())
			case dto.MetricType_UNTYPED:
				add(name, lp, m.GetUntyped().GetValue())
			case dto.MetricType_HISTOGRAM, dto.MetricType_GAUGE_HISTOGRAM:
				h := m.GetHistogram()
				for _, b := range h.GetBucket() {
					add(name+"_bucket", lp, float64(b.GetCumulativeCount()), Label{"le", formatFloat(b.GetUpperBound())})
				}
				add(name+"_bucket", lp, float64(h.GetSampleCount()), Label{"le", "+Inf"})
				add(name+"_sum", lp, h.GetSampleSum())
				add(name+"_count", lp, float64(h.GetSampleCount()))
			case dto.MetricType_SUMMARY:
				s := m.GetSummary()
				for _, q := range s.GetQuantile() {
					add(name, lp, q.GetValue(), Label{"quantile", formatFloat(q.GetQuantile())})
				}
				add(name+"_sum", lp, s.GetSampleSum())
				add(name+"_count", lp, float64(s.GetSampleCount()))
			}
		}
	}
	return out
}

func formatFloat(f float64) string {
	if math.IsInf(f, +1) {
		return "+Inf"
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// EncodeWriteRequest serialises series as a prometheus.WriteRequest protobuf:
//
//	message WriteRequest { repeated TimeSeries timeseries = 1; }
//	message TimeSeries   { repeated Label labels = 1; repeated Sample samples = 2; }
//	message Label        { string name = 1; string value = 2; }
//	message Sample       { double value = 1; int64 timestamp = 2; }
func EncodeWriteRequest(series []TimeSeries) []byte {
	var buf []byte
	for _, s := range series {
		var tsBuf []byte
		for _, l := range s.Labels {
			var lb []byte
			lb = protowire.AppendTag(lb, 1, protowire.BytesType)
			lb = protowire.AppendString(lb, l.Name)
			lb = protowire.AppendTag(lb, 2, protowire.BytesType)
			lb = protowire.AppendString(lb, l.Value)
			tsBuf = protowire.AppendTag(tsBuf, 1, protowire.BytesType)
			tsBuf = protowire.AppendBytes(tsBuf, lb)
		}
		for _, smp := range s.Samples {
			var sb []byte
			sb = protowire.AppendTag(sb, 1, protowire.Fixed64Type)
			sb = protowire.AppendFixed64(sb, math.Float64bits(smp.Value))
			sb = protowire.AppendTag(sb, 2, protowire.VarintType)
			sb = protowire.AppendVarint(sb, uint64(smp.Timestamp))
			tsBuf = protowire.AppendTag(tsBuf, 2, protowire.BytesType)
			tsBuf = protowire.AppendBytes(tsBuf, sb)
		}
		buf = protowire.AppendTag(buf, 1, protowire.BytesType)
		buf = protowire.AppendBytes(buf, tsBuf)
	}
	return buf
}
