"""Generates deploy/grafana/dashboards/pulseops.json.

Usage: python3 deploy/grafana/generate_dashboard.py
The JSON is committed; re-run this after changing panels."""
import json, os

DS = {"type": "prometheus", "uid": "${datasource}"}
panels = []
pid = 0

def nid():
    global pid
    pid += 1
    return pid

def target(expr, legend="", ref="A", instant=False):
    t = {"datasource": DS, "expr": expr, "legendFormat": legend, "refId": ref, "range": not instant, "instant": instant}
    return t

def stat(title, expr, x, y, unit="none", thresholds=None, desc="", decimals=None, color_mode="value"):
    p = {
        "id": nid(), "type": "stat", "title": title, "description": desc, "datasource": DS,
        "gridPos": {"h": 4, "w": 6, "x": x, "y": y},
        "targets": [target(expr, instant=True)],
        "options": {"reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False},
                    "colorMode": color_mode, "graphMode": "none", "textMode": "value", "justifyMode": "auto"},
        "fieldConfig": {"defaults": {"unit": unit, "thresholds": thresholds or {"mode": "absolute", "steps": [{"color": "text", "value": None}]}}, "overrides": []},
    }
    if decimals is not None:
        p["fieldConfig"]["defaults"]["decimals"] = decimals
    return p

def ts(title, targets, x, y, w=12, h=8, unit="none", desc="", stack=False, draw="line"):
    return {
        "id": nid(), "type": "timeseries", "title": title, "description": desc, "datasource": DS,
        "gridPos": {"h": h, "w": w, "x": x, "y": y},
        "targets": targets,
        "options": {"legend": {"displayMode": "list", "placement": "bottom", "showLegend": True},
                    "tooltip": {"mode": "multi", "sort": "desc"}},
        "fieldConfig": {"defaults": {"unit": unit, "custom": {
            "drawStyle": draw, "lineWidth": 2, "fillOpacity": 10, "showPoints": "never",
            "stacking": {"mode": "normal" if stack else "none", "group": "A"}, "spanNulls": False}}, "overrides": []},
    }

def row(title, y):
    return {"id": nid(), "type": "row", "title": title, "collapsed": False, "gridPos": {"h": 1, "w": 24, "x": 0, "y": y}, "panels": []}

y = 0
panels.append(row("Overview", y)); y += 1
panels.append(stat("Checks reachable", 'sum(pulseops_check_up) / count(pulseops_check_up)', 0, y, unit="percentunit", decimals=1,
    desc="Share of checks whose last probe reached the target.",
    thresholds={"mode": "absolute", "steps": [{"color": "red", "value": None}, {"color": "orange", "value": 0.9}, {"color": "green", "value": 0.999}]}))
panels.append(stat("Active incidents", 'sum(pulseops_incidents_active)', 6, y,
    thresholds={"mode": "absolute", "steps": [{"color": "green", "value": None}, {"color": "red", "value": 1}]}))
panels.append(stat("Probe success (1h)", '(sum(increase(pulseops_probes_total{outcome!="down"}[1h])) or vector(0)) / sum(increase(pulseops_probes_total[1h]))', 12, y,
    unit="percentunit", decimals=2,
    thresholds={"mode": "absolute", "steps": [{"color": "red", "value": None}, {"color": "orange", "value": 0.99}, {"color": "green", "value": 0.999}]}))
panels.append(stat("Failed notifications (24h)", 'sum(increase(pulseops_notifications_total{result="failed"}[24h])) or vector(0)', 18, y, decimals=0,
    thresholds={"mode": "absolute", "steps": [{"color": "green", "value": None}, {"color": "red", "value": 1}]}))
y += 4

panels.append(row("Probes", y)); y += 1
panels.append(ts("Probe latency p95 by check",
    [target('histogram_quantile(0.95, sum by (le, check) (rate(pulseops_probe_duration_seconds_bucket[$__rate_interval])))', "{{check}}")],
    0, y, unit="s", desc="95th percentile probe duration per check (connect + TLS + time to first byte)."))
panels.append(ts("Check reachability",
    [target('pulseops_check_up', "{{check}}")], 12, y, unit="bool_yes_no", draw="line",
    desc="1 when the last probe reached the target, 0 when it failed."))
y += 8
panels.append(ts("Probe outcomes",
    [target('sum by (outcome) (rate(pulseops_probes_total[$__rate_interval]))', "{{outcome}}")], 0, y, unit="ops", stack=True,
    desc="Probe throughput split into up / degraded / down."))
panels.append(ts("Scheduler lag p50 / p95",
    [target('histogram_quantile(0.5, sum by (le) (rate(pulseops_scheduler_lag_seconds_bucket[$__rate_interval])))', "p50"),
     target('histogram_quantile(0.95, sum by (le) (rate(pulseops_scheduler_lag_seconds_bucket[$__rate_interval])))', "p95", "B")],
    12, y, unit="s", desc="How late probes start relative to their schedule. Rising lag means the worker pool is saturated."))
y += 8

panels.append(row("Incidents & notifications", y)); y += 1
panels.append(ts("Incidents opened / resolved",
    [target('sum by (cause) (increase(pulseops_incidents_opened_total[$__rate_interval]))', "opened: {{cause}}"),
     target('sum by (how) (increase(pulseops_incidents_resolved_total[$__rate_interval]))', "resolved: {{how}}", "B")],
    0, y, draw="bars", desc="Incident lifecycle events from the alert engine and operators."))
panels.append(ts("Notification deliveries",
    [target('sum by (provider, result) (increase(pulseops_notifications_total[$__rate_interval]))', "{{provider}} {{result}}")],
    12, y, draw="bars", stack=True, desc="Outbox deliveries by provider (slack/mock) and result (sent/retry/failed)."))
y += 8

panels.append(row("API & runtime", y)); y += 1
panels.append(ts("API requests by route",
    [target('sum by (route) (rate(pulseops_http_requests_total{route!="/healthz", route!="/metrics"}[$__rate_interval]))', "{{route}}")],
    0, y, unit="reqps"))
panels.append(ts("API latency p95 by route",
    [target('histogram_quantile(0.95, sum by (le, route) (rate(pulseops_http_request_duration_seconds_bucket{route!="/metrics"}[$__rate_interval])))', "{{route}}")],
    12, y, unit="s"))
y += 8
panels.append(ts("API 5xx ratio",
    [target('(sum(rate(pulseops_http_requests_total{code=~"5.."}[$__rate_interval])) or vector(0)) / sum(rate(pulseops_http_requests_total[$__rate_interval]))', "5xx ratio")],
    0, y, unit="percentunit"))
panels.append(ts("Go runtime",
    [target('sum(go_goroutines{job="pulseops-api"})', "goroutines"),
     target('sum(process_resident_memory_bytes{job="pulseops-api"}) / 1024 / 1024', "RSS MiB", "B")],
    12, y))

dash = {
    "uid": "pulseops-overview",
    "title": "PulseOps — platform overview",
    "description": "Probe health, alerting pipeline and API metrics exported by apps/api (/metrics or remote_write).",
    "tags": ["pulseops"],
    "timezone": "browser",
    "editable": True,
    "graphTooltip": 1,
    "refresh": "30s",
    "schemaVersion": 39,
    "version": 1,
    "time": {"from": "now-6h", "to": "now"},
    "templating": {"list": [{
        "name": "datasource", "label": "Data source", "type": "datasource", "query": "prometheus",
        "current": {"text": "Prometheus", "value": "prometheus"}, "hide": 0, "refresh": 1, "regex": "", "includeAll": False, "multi": False,
    }]},
    "annotations": {"list": []},
    "links": [],
    "panels": panels,
}
out = os.path.join(os.path.dirname(os.path.abspath(__file__)), "dashboards", "pulseops.json")
with open(out, "w") as f:
    json.dump(dash, f, indent=2)
    f.write("\n")
print(len(panels), "panels")
