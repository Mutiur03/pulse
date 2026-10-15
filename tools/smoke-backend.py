#!/usr/bin/env python3
"""Backend half of the smoke test.

Sends one OTLP/HTTP JSON trace that spans browser -> API -> database with an
exception on the API span, then checks through the query API that:
  * the exception became an issue (new_issue published to issue.events),
  * GET /v1/traces/{id} returns browser, backend and DB spans in one tree,
  * resolving the issue and sending the error again reopens it (regression).
Optionally (DEMO_API_URL) it calls the instrumented Go demo API and checks
that its protobuf-exported spans and SQL span land in the same tree.
Exits non-zero on the first failed check.
"""
import json
import os
import random
import subprocess
import sys
import time
import urllib.request

INGEST = os.environ.get("INGEST_URL", "http://localhost:8080")
QUERY = os.environ.get("QUERY_URL", "http://localhost:8081")
DEMO = os.environ.get("DEMO_API_URL", "")
TIMEOUT = float(os.environ.get("SMOKE_TIMEOUT", "90"))
KAFKA = os.environ.get("KAFKA_CONTAINER", "kafka")


def fail(msg):
    print(f"FAIL: {msg}", file=sys.stderr)
    sys.exit(1)


def ok(msg):
    print(f"ok: {msg}")


def http(method, url, body=None, headers=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(url, data=data, method=method, headers={"Content-Type": "application/json", **(headers or {})})
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            raw = r.read()
            return r.status, json.loads(raw) if raw else None
    except urllib.error.HTTPError as e:
        raw = e.read()
        try:
            return e.code, json.loads(raw)
        except ValueError:
            return e.code, raw.decode(errors="replace")


def wait(desc, fn):
    deadline = time.time() + TIMEOUT
    last = None
    while time.time() < deadline:
        try:
            last = fn()
            if last:
                return last
        except Exception as e:  # noqa: BLE001 - keep polling on transient errors
            last = e
        time.sleep(2)
    fail(f"timed out waiting for {desc} (last: {last})")


def hexid(n):
    return "".join(random.choice("0123456789abcdef") for _ in range(n))


def attr(k, v):
    return {"key": k, "value": {"stringValue": v}}


def resource(service, lang):
    return {"attributes": [attr("service.name", service), attr("telemetry.sdk.language", lang), attr("pulse.project_id", "1")]}


def span(trace, sid, parent, name, kind, start, dur_ms, attrs=(), events=(), status=1):
    return {
        "traceId": trace, "spanId": sid, "parentSpanId": parent, "name": name, "kind": kind,
        "startTimeUnixNano": str(start), "endTimeUnixNano": str(start + dur_ms * 1_000_000),
        "attributes": list(attrs), "events": list(events), "status": {"code": status},
    }


def send_trace(trace, run, ids, t0):
    click, fetch, api, db = ids
    stack = (f"main.checkout_{run}(0xc000010000)\n\t/app/checkout.go:42 +0x1d\n"
             "net/http.HandlerFunc.ServeHTTP(0x0)\n\t/usr/local/go/src/net/http/server.go:2166 +0x29")
    payload = {"resourceSpans": [
        {"resource": resource("smoke-browser", "webjs"), "scopeSpans": [{"spans": [
            span(trace, click, "", "click", 1, t0, 120, [attr("session.id", f"sess_{run}")]),
            span(trace, fetch, click, "HTTP POST", 3, t0 + 5_000_000, 100, [attr("url.full", "http://api.local/api/checkout")]),
        ]}]},
        {"resource": resource("smoke-api", "go"), "scopeSpans": [{"spans": [
            span(trace, api, fetch, "POST /api/checkout", 2, t0 + 10_000_000, 80,
                 [attr("http.route", "/api/checkout"), {"key": "http.response.status_code", "value": {"intValue": "500"}}],
                 [{"name": "exception", "timeUnixNano": str(t0 + 80_000_000), "attributes": [
                     attr("exception.type", "SmokeError"),
                     attr("exception.message", f"checkout failed for order {random.randint(1000, 9999)}"),
                     attr("exception.stacktrace", stack)]}],
                 status=2),
            span(trace, db, api, "SELECT orders", 3, t0 + 20_000_000, 30,
                 [attr("db.system", "postgresql"), attr("db.statement", "SELECT * FROM orders WHERE id = $1")]),
        ]}]},
    ]}
    status, body = http("POST", f"{INGEST}/v1/traces", payload)
    if status != 202:
        fail(f"OTLP trace rejected: {status} {body}")


def sources(tree):
    out = []

    def walk(n, path):
        out.append((n["source"], n["name"], path))
        for c in n["children"]:
            walk(c, path + [n["source"]])
    for r in tree["roots"]:
        walk(r, [])
    return out


def kafka_has(issue_id, kind):
    try:
        out = subprocess.run(
            ["docker", "exec", KAFKA, "/opt/kafka/bin/kafka-console-consumer.sh", "--bootstrap-server", "localhost:9092",
             "--topic", "issue.events", "--from-beginning", "--timeout-ms", "8000"],
            capture_output=True, text=True, timeout=60).stdout
    except (FileNotFoundError, subprocess.TimeoutExpired):
        return None
    for line in out.splitlines():
        try:
            e = json.loads(line)
        except ValueError:
            continue
        if e.get("issue_id") == issue_id and e.get("type") == kind:
            return True
    return False


def main():
    run = f"{int(time.time())}{random.randint(100, 999)}"
    trace = hexid(32)
    ids = [hexid(16) for _ in range(4)]
    t0 = time.time_ns()
    print(f"==> backend smoke run {run}, trace {trace}")
    send_trace(trace, run, ids, t0)
    ok("OTLP trace with exception accepted")

    def find_issue():
        _, body = http("GET", f"{QUERY}/v1/issues?query=checkout_{run}&limit=5")
        return body["issues"][0] if body and body.get("issues") else None
    issue = wait("issue", find_issue)
    if issue["sample_trace_id"] != trace or issue["service_name"] != "smoke-api" or issue["status"] != "unresolved":
        fail(f"unexpected issue: {issue}")
    ok(f"issue {issue['id']} created: {issue['title']} ({issue['culprit']})")

    def full_tree():
        status, body = http("GET", f"{QUERY}/v1/traces/{trace}")
        return body if status == 200 and body["span_count"] == 4 and body["error_count"] >= 1 else None
    tree = wait("trace tree", full_tree)
    if len(tree["roots"]) != 1:
        fail(f"want one root, got {len(tree['roots'])}")
    got = sources(tree)
    want = [("browser", "click", []), ("browser", "HTTP POST", ["browser"]),
            ("backend", "POST /api/checkout", ["browser", "browser"]),
            ("database", "SELECT orders", ["browser", "browser", "backend"])]
    if got != want:
        fail(f"tree shape {got}")
    api_node = tree["roots"][0]["children"][0]["children"][0]
    if not api_node["errors"] or api_node["errors"][0]["issue_id"] != issue["id"]:
        fail("exception not attached to the API span")
    ok("trace tree: browser click -> browser fetch -> backend API -> database, error on the API span")

    status, detail = http("GET", f"{QUERY}/v1/issues/{issue['id']}")
    if status != 200 or not detail["recent_events"] or detail["recent_events"][0]["trace_id"] != trace:
        fail(f"issue detail: {status} {detail}")
    ok("issue detail lists the occurrence")

    status, body = http("PATCH", f"{QUERY}/v1/issues/{issue['id']}", {"status": "resolved"})
    if status != 200 or body["status"] != "resolved":
        fail(f"resolve: {status} {body}")
    time.sleep(1)
    trace2 = hexid(32)
    send_trace(trace2, run, [hexid(16) for _ in range(4)], time.time_ns())

    def reopened():
        _, b = http("GET", f"{QUERY}/v1/issues/{issue['id']}")
        i = b["issue"]
        return i if i["status"] == "unresolved" and i["times_regressed"] == 1 and i["count"] == 2 else None
    i = wait("regression", reopened)
    ok(f"resolved issue reopened on a new event (count {i['count']}, regressed {i['times_regressed']}x)")

    for kind in ("new_issue", "regression"):
        r = kafka_has(issue["id"], kind)
        if r is None:
            print(f"skip: cannot read issue.events without docker ({kind})")
        elif not r:
            fail(f"{kind} event for issue {issue['id']} not on issue.events")
        else:
            ok(f"{kind} event published to issue.events")

    if DEMO:
        demo_trace = hexid(32)
        status, body = http("POST", f"{DEMO}/api/pay", {}, {"traceparent": f"00-{demo_trace}-{hexid(16)}-01", "baggage": f"session.id=sess_{run}"})
        if status != 402:
            fail(f"demo /api/pay: {status} {body}")

        def demo_tree():
            s, b = http("GET", f"{QUERY}/v1/traces/{demo_trace}")
            if s != 200:
                return None
            srcs = {x[0] for x in sources(b)}
            return b if {"backend", "database"} <= srcs and b["error_count"] >= 1 else None
        b = wait("go-demo-api trace", demo_tree)
        ok(f"go-demo-api (OTLP protobuf) trace: {b['span_count']} spans, {b['error_count']} error, services {b['services']}")

    print("PASS: backend traces, error grouping, regressions and trace tree")


if __name__ == "__main__":
    main()
