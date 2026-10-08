#!/usr/bin/env python3
"""Exercise the isolated Compose stack through its public Nginx endpoint.

Start deploy/compose.test.yaml first, then run this script with Python 3.
It creates disposable users and mock channels only in the named test project.
PostgreSQL and Redis are briefly paused to verify settlement and admission.
No production payment credentials or external model endpoint is used.
"""

import argparse
import gzip
import json
import re
import secrets
import subprocess
import threading
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from acceptance_restored import verify_restored, verify_live_status
from acceptance_subscription_v2 import verify_subscription_v2


class Upstream(BaseHTTPRequestHandler):
    calls = 0
    calls_lock = threading.Lock()

    def log_message(self, *_args):
        pass

    def do_POST(self):
        with Upstream.calls_lock:
            Upstream.calls += 1
        if self.path not in ("/v1/chat/completions", "/v1/responses") or self.headers.get("Authorization") != "Bearer fixture-secret":
            self.send_response(400)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(b'{"error":{"message":"wrong upstream endpoint or credential"}}')
            return
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))))
        if self.path.endswith("/responses"):
            payload = {"id": f"resp_fixture_{Upstream.calls}", "object": "response", "status": "completed", "model": body["model"], "output": [{"id": "msg_fixture", "type": "message", "status": "completed", "role": "assistant", "content": [{"type": "output_text", "text": "mock-ok", "annotations": []}]}], "usage": {"input_tokens": 2, "output_tokens": 3, "total_tokens": 5}}
            self.send_response(200)
            if body.get("stream"):
                self.send_header("Content-Type", "text/event-stream")
                self.end_headers()
                events = [
                    {"type": "response.created", "sequence_number": 0, "response": {"id": payload["id"], "status": "in_progress", "output": []}},
                    {"type": "response.output_text.delta", "sequence_number": 1, "delta": "mock-ok", "response_id": payload["id"]},
                    {"type": "response.completed", "sequence_number": 2, "response": payload},
                ]
                for event in events:
                    self.wfile.write(f"event: {event['type']}\ndata: {json.dumps(event)}\n\n".encode())
                self.wfile.flush()
                return
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps(payload).encode())
            return
        if body.get("messages", [{}])[0].get("content") == "force-error":
            self.send_response(503)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(b'{"error":{"message":"test upstream unavailable"}}')
            return
        if body.get("stream"):
            chunks = [
                {"id": "test-chat", "object": "chat.completion.chunk", "model": body["model"], "choices": [{"index": 0, "delta": {"role": "assistant", "content": "mock-ok"}, "finish_reason": None}]},
                {"id": "test-chat", "object": "chat.completion.chunk", "model": body["model"], "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}], "usage": {"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 5}},
            ]
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()
            for chunk in chunks:
                self.wfile.write(("data: " + json.dumps(chunk) + "\n\n").encode())
                self.wfile.flush()
            self.wfile.write(b"data: [DONE]\n\n")
        else:
            payload = {"id": "test-chat", "object": "chat.completion", "model": body["model"], "choices": [{"index": 0, "message": {"role": "assistant", "content": "mock-ok"}, "finish_reason": "stop"}], "usage": {"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 5}}
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps(payload).encode())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", default="codego-v3-acceptance")
    parser.add_argument("--url", default="http://localhost:18083")
    parser.add_argument("--fixture-port", type=int, default=18185)
    args = parser.parse_args()
    if not re.fullmatch(r"codego-v3-[a-z0-9-]+", args.project):
        parser.error("only explicitly named codego-v3-* test projects are permitted")
    pg = args.project + "-postgres-1"
    inspected = json.loads(subprocess.check_output(["docker", "inspect", pg], text=True))[0]
    if inspected["Config"]["Labels"].get("com.docker.compose.project") != args.project:
        parser.error("PostgreSQL does not belong to the selected isolated Compose project")
    fixture = ThreadingHTTPServer(("0.0.0.0", args.fixture_port), Upstream)
    threading.Thread(target=fixture.serve_forever, daemon=True).start()
    passed = []

    def sql(statement):
        return subprocess.check_output(["docker", "exec", pg, "psql", "-U", "codego", "-d", "codego", "-At", "-v", "ON_ERROR_STOP=1", "-c", statement], text=True).strip()

    def request(path, method="GET", body=None, token=None, origin=None, headers=None):
        h = {"Content-Type": "application/json", "X-CodeGo-API-Version": "3"}
        if token:
            h["Authorization"] = "Bearer " + token
        if origin:
            h["Origin"] = origin
        h.update(headers or {})
        data = None if body is None else (body.encode() if isinstance(body, str) else json.dumps(body).encode())
        req = urllib.request.Request(args.url + path, data=data, method=method, headers=h)
        try:
            res = urllib.request.urlopen(req, timeout=15)
        except urllib.error.HTTPError as err:
            res = err
        raw = res.read()
        try:
            payload = json.loads(raw)
        except (ValueError, UnicodeDecodeError):
            payload = raw
        return res.status, payload, res.headers

    def check(name, result, expected=200):
        status, payload, _headers = result
        if status != expected:
            # Never print successful session/key payloads or request credentials.
            message = payload.get("message", payload.get("error", "")) if isinstance(payload, dict) else ""
            raise AssertionError(f"{name}: expected HTTP {expected}, got {status}; {message}")
        passed.append(name)
        print("PASS", name, flush=True)
        return payload

    try:
        check("Nginx health", request("/healthz"))
        spec = check("OpenAPI served", request("/api/openapi.json"))
        assert spec["openapi"].startswith("3.")
        _, page, _ = request("/")
        js = re.search(rb'src="([^"]+\.js)"', page).group(1).decode()
        js = js if js.startswith("/") else "/" + js
        _, zipped, headers = request(js, headers={"Accept-Encoding": "gzip"})
        assert headers.get("Content-Encoding") == "gzip" and gzip.decompress(zipped)
        plain_js = gzip.decompress(zipped)
        passed.append("precompressed frontend assets")
        print("PASS precompressed frontend assets", flush=True)
        for encoding, expected in [("br;q=0, gzip", "gzip"), ("gzip;q=0, br", "br"), ("br;q=0, gzip;q=0", None), ("br;q=0, *;q=1", "gzip"), ("identity", None)]:
            _, encoded, response_headers = request(js, headers={"Accept-Encoding": encoding})
            assert response_headers.get("Content-Encoding") == expected, f"encoding negotiation failed for {encoding}"
            if expected is None:
                assert encoded == plain_js
            elif expected == "gzip":
                assert gzip.decompress(encoded) == plain_js
        passed.append("encoding quality zero is excluded")
        print("PASS encoding quality zero is excluded", flush=True)
        check("anonymous identity rejected", request("/api/user/self"), 401)
        suffix = secrets.token_hex(4)
        username, password = "accept_" + suffix, secrets.token_urlsafe(24)
        policies = check("published current policy versions", request("/api/policies/current"))["data"]
        agreements = {"accepted_terms_version": policies["version"], "accepted_privacy_version": policies["version"], "agreement_locale": "en"}
        registration = {"username": username, "password": password, **agreements}
        session = check("register and issue session", request("/api/user/register", "POST", registration, origin=args.url))["data"]
        token, user_id = session["access_token"], session["user"]["id"]
        check("duplicate registration rejected", request("/api/user/register", "POST", registration), 409)
        check("short password rejected", request("/api/user/register", "POST", {"username": "bad_" + suffix, "password": "short"}), 400)
        check("session authenticated", request("/api/user/self", token=token))
        check("ordinary user cannot administer catalog", request("/api/catalog/channels", token=token), 403)
        sql(f"UPDATE v3_identity.users SET role='root' WHERE id={user_id}")
        key = check("create scoped API key", request("/api/token/", "POST", {"name": "acceptance", "allowed_models": ["acceptance-chat"]}, token=token, origin=args.url))["data"]
        raw_key, key_id = key["key"], key["id"]
        check("forged group rejected", request("/api/token/", "POST", {"name": "forged", "group": "admin"}, token=token), 403)
        other = check("register second owner", request("/api/user/register", "POST", {"username": "other_" + suffix, "password": password, **agreements}))["data"]
        check("API key ownership enforced", request(f"/api/token/{key_id}/key", token=other["access_token"]), 404)
        wallet = check("wallet created", request("/api/billing/balance", token=token))["data"]["account_id"]
        adjustment = {"account_id": wallet, "amount_micro": 1000000, "operation_id": "accept-" + suffix, "reason": "isolated acceptance fixture"}
        check("ledger credit", request("/api/billing/adjustments", "POST", adjustment, token=token, origin=args.url))
        duplicate = check("ledger idempotency", request("/api/billing/adjustments", "POST", adjustment, token=token))["data"]
        assert duplicate["duplicate"]
        check("conflicting ledger replay rejected", request("/api/billing/adjustments", "POST", {**adjustment, "amount_micro": 1}, token=token), 409)
        check("configure group with browser Origin", request("/api/catalog/groups/default", "PUT", {"multiplier": 1}, token=token, origin=args.url))
        check("configure exact token price", request("/api/catalog/prices/acceptance-chat", "PUT", {"input_per_mtok": 1000000, "output_per_mtok": 2000000}, token=token, origin=args.url))
        check("configure forbidden model price", request("/api/catalog/prices/forbidden", "PUT", {"input_per_mtok": 1000000, "output_per_mtok": 2000000}, token=token, origin=args.url))
        channel = {"name": "acceptance-" + suffix, "provider": "openai", "base_url": f"http://host.docker.internal:{args.fixture_port}", "weight": 1, "groups": ["default"], "models": ["acceptance-chat", "forbidden"], "credentials": [{"secret": "fixture-secret"}]}
        channel_result = check("create mock channel with encrypted credential", request("/api/catalog/channels", "POST", channel, token=token, origin=args.url))
        assert "fixture-secret" not in json.dumps(channel_result)
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            status, models, _ = request("/v1/models", token=raw_key)
            if status == 200 and any(m.get("id") == "acceptance-chat" for m in models.get("data", [])):
                break
            time.sleep(.3)
        else:
            raise AssertionError(f"model discovery did not expose the configured route; last GET /v1/models HTTP {status}")
        assert not any(m.get("id") == "forbidden" for m in models["data"]), "scoped key must not discover forbidden models"
        passed.append("catalog invalidation reaches gateway")
        print("PASS catalog invalidation reaches gateway", flush=True)
        chat = {"model": "acceptance-chat", "messages": [{"role": "user", "content": "hello"}], "max_tokens": 16}
        result = check("OpenAI nonstream relay", request("/v1/chat/completions", "POST", chat, token=raw_key))
        assert result["choices"][0]["message"]["content"] == "mock-ok"
        streamed = check("OpenAI SSE relay", request("/v1/chat/completions", "POST", {**chat, "stream": True}, token=raw_key))
        assert b"mock-ok" in streamed and b"[DONE]" in streamed
        response = check("OpenAI Responses relay", request("/v1/responses", "POST", {"model": "acceptance-chat", "input": "hello", "max_output_tokens": 16}, token=raw_key))
        assert response["output"][0]["content"][0]["text"] == "mock-ok"
        anthropic = check("Anthropic caller conversion", request("/v1/messages", "POST", chat, token=raw_key))
        assert anthropic["content"][0]["text"] == "mock-ok"
        gemini = check("Gemini caller conversion", request("/v1beta/models/acceptance-chat:generateContent", "POST", {"contents": [{"role": "user", "parts": [{"text": "hello"}]}], "generationConfig": {"maxOutputTokens": 16}}, token=raw_key))
        assert gemini["candidates"][0]["content"]["parts"][0]["text"] == "mock-ok"
        check("malformed JSON rejected", request("/v1/chat/completions", "POST", "{", token=raw_key), 400)
        check("model scope enforced", request("/v1/chat/completions", "POST", {**chat, "model": "forbidden"}, token=raw_key), 403)
        ip_key = check("create IP scoped key", request("/api/token/", "POST", {"name": "IP restricted", "allowed_cidrs": ["203.0.113.0/24"]}, token=token))["data"]
        check("API key client IP scope enforced", request("/v1/chat/completions", "POST", chat, token=ip_key["key"]), 403)
        check("untrusted forwarded IP cannot bypass key restriction", request("/v1/chat/completions", "POST", chat, token=ip_key["key"], headers={"X-Forwarded-For": "203.0.113.1", "X-Real-IP": "203.0.113.1"}), 403)
        check("invalid API key rejected", request("/v1/models", token="sk-invalid"), 401)
        # Group and identity writes above enqueue asynchronous invalidations.
        # Drain them before warming the original key so this exercises a warm
        # request, rather than a cold key whose profile requires PostgreSQL.
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            if sql("SELECT count(*) FROM v3_platform.cache_invalidation_outbox") == "0":
                break
            time.sleep(.1)
        else:
            raise AssertionError("cache invalidations did not drain before outage test")
        time.sleep(.3)  # allow published invalidations to reach subscribers
        check("original API key warmed after configuration invalidations", request("/v1/models", token=raw_key))
        subprocess.run(["docker", "pause", pg], check=True, stdout=subprocess.DEVNULL)
        try:
            delayed = check("warm gateway request succeeds while PostgreSQL is unavailable", request("/v1/chat/completions", "POST", chat, token=raw_key))
            assert delayed["choices"][0]["message"]["content"] == "mock-ok"
        finally:
            subprocess.run(["docker", "unpause", pg], check=True, stdout=subprocess.DEVNULL)
        failure = request("/v1/chat/completions", "POST", {**chat, "messages": [{"role": "user", "content": "force-error"}]}, token=raw_key)
        assert failure[0] in (502, 503), f"upstream failure HTTP {failure[0]}"
        passed.append("upstream failure returned")
        print("PASS upstream failure returned", flush=True)
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            balance = request("/api/billing/balance", token=token)[1]["data"]["balance_micro"]
            if balance == 999952:
                break
            time.sleep(.3)
        assert balance == 999952, f"exact settlement expected 999952 micro, got {balance}"
        passed.append("async ledger settles 6 x 8 micro after database recovery and refunds failure")
        print("PASS async ledger settles 6 x 8 micro after database recovery and refunds failure", flush=True)
        check("API key warm before Redis outage", request("/v1/models", token=raw_key))
        rd = args.project + "-redis-1"
        redis_info = json.loads(subprocess.check_output(["docker", "inspect", rd], text=True))[0]
        assert redis_info["Config"]["Labels"].get("com.docker.compose.project") == args.project
        calls_before = Upstream.calls
        financial_before = sql(f"SELECT balance,(SELECT count(*) FROM v3_billing.usage_logs WHERE user_id={user_id}) FROM v3_billing.accounts WHERE id={wallet}")
        subprocess.run(["docker", "pause", rd], check=True, stdout=subprocess.DEVNULL)
        try:
            unavailable = check("Redis outage refuses new admission", request("/v1/chat/completions", "POST", chat, token=raw_key), 503)
            assert unavailable["error"]["code"] == "billing_unavailable"
            assert Upstream.calls == calls_before, "Redis outage must not reach upstream"
        finally:
            subprocess.run(["docker", "unpause", rd], check=True, stdout=subprocess.DEVNULL)
        assert sql(f"SELECT balance,(SELECT count(*) FROM v3_billing.usage_logs WHERE user_id={user_id}) FROM v3_billing.accounts WHERE id={wallet}") == financial_before
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            reserved = subprocess.check_output(["docker", "exec", rd, "redis-cli", "--raw", "HGET", f"v3:bal:{{{wallet}}}", "reserved"], text=True).strip()
            if reserved == "0":
                break
            time.sleep(.2)
        assert reserved == "0", f"rejected Redis outage request left {reserved} micro reserved after recovery"
        check("Redis recovery preserves model access and no financial mutation", request("/v1/models", token=raw_key))
        check("usage audit available", request("/api/log/self", token=token))
        check("order listing available", request("/api/commerce/orders", token=token))
        check("subscription listing available", request("/api/subscription/self", token=token))
        redemption = check("issue redemption code", request("/api/redemption/", "POST", {"name": "acceptance-" + suffix, "credits": 100}, token=token, origin=args.url))["data"]
        redeem_body = {"key": redemption["key"]}
        credited = check("redeem code into wallet", request("/api/commerce/redemptions/redeem", "POST", redeem_body, token=token))["data"]
        credited_replay = check("redemption replay does not duplicate credit", request("/api/commerce/redemptions/redeem", "POST", redeem_body, token=token))["data"]
        assert credited == credited_replay and credited["redeem_type"] == "credits" and credited["credits"] == 100
        check("redemption cannot be claimed by another owner", request("/api/commerce/redemptions/redeem", "POST", redeem_body, token=other["access_token"]), 409)
        codes = check("redemption listing hides raw codes", request("/api/redemption/", token=token))
        assert redemption["key"] not in json.dumps(codes)
        check("group buy listing available", request("/api/group-buy/list", token=token))
        check("blind box inventory available", request("/api/blind-box/self", token=token))
        check("empty blind box inventory rejected", request("/api/blind-box/inventory/open", "POST", {"request_id": "empty-" + suffix, "count": 1}, token=token), 409)
        pool_body = {"name": "acceptance-" + suffix, "enabled": True, "price_micro": 100, "daily_limit": 2, "rewards": [{"kind": "credits", "title": "fixture", "weight": 1, "amount_micro": 50}]}
        check("ordinary user cannot configure blind box pool", request("/api/blind-box/admin/pools", "PUT", pool_body, token=other["access_token"]), 403)
        pool = check("configure deterministic blind box reward", request("/api/blind-box/admin/pools", "PUT", pool_body, token=token, origin=args.url))["data"]
        buy = {"request_id": "buy-" + suffix, "pool_id": pool["id"], "count": 2}
        purchase = check("purchase blind boxes from wallet", request("/api/blind-box/inventory/purchase", "POST", buy, token=token))["data"]
        purchase_replay = check("blind box purchase replay", request("/api/blind-box/inventory/purchase", "POST", buy, token=token))["data"]
        assert purchase_replay["id"] == purchase["id"]
        check("blind box daily limit enforced", request("/api/blind-box/inventory/purchase", "POST", {**buy, "request_id": "limit-" + suffix, "count": 1}, token=token), 429)
        opened_body = {"request_id": "open-" + suffix, "count": 1}
        opened = check("open box and credit reward", request("/api/blind-box/inventory/open", "POST", opened_body, token=token))["data"]
        replay_open = check("box reward replay is idempotent", request("/api/blind-box/inventory/open", "POST", opened_body, token=token))["data"]
        assert opened == replay_open and opened[0]["reward"]["amount_micro"] == 50
        gift = {"request_id": "gift-" + suffix, "recipient_id": other["user"]["id"], "count": 1}
        check("gift remaining box", request("/api/blind-box/inventory/gift", "POST", gift, token=token))
        check("gift replay is idempotent", request("/api/blind-box/inventory/gift", "POST", gift, token=token))
        check("recipient opens gifted box", request("/api/blind-box/inventory/open", "POST", {"request_id": "receive-" + suffix, "count": 1}, token=other["access_token"]))
        assert request("/api/billing/balance", token=token)[1]["data"]["balance_micro"] == 999902
        assert request("/api/billing/balance", token=other["access_token"])[1]["data"]["balance_micro"] == 50
        passed.append("blind box purchase and rewards preserve exact money across owners")
        print("PASS blind box purchase and rewards preserve exact money across owners", flush=True)
        check("disable fixture reward pool", request("/api/blind-box/admin/pools", "PUT", {**pool, "enabled": False}, token=token, origin=args.url))
        plan = check("create redeemable subscription", request("/api/subscription/admin/plans", "POST", {"name": "redeem-" + suffix, "currency": "usd", "price_minor": 100, "credits": 5000, "period_seconds": 86400, "reset_period": "never", "enabled": True, "model_limits": {"acceptance-chat": 700}}, token=token, origin=args.url))["data"]
        sub_code = check("issue subscription redemption", request("/api/redemption/", "POST", {"name": "sub-" + suffix, "redeem_type": "subscription", "plan_id": plan["id"]}, token=token, origin=args.url))["data"]
        sub_body = {"key": sub_code["key"]}
        sub_result = check("redeem subscription entitlement", request("/api/commerce/redemptions/redeem", "POST", sub_body, token=token))["data"]
        sub_replay = check("subscription redemption replay", request("/api/user/topup", "POST", sub_body, token=token))["data"]
        assert sub_result == sub_replay and sub_result["redeem_type"] == "subscription" and sub_result["credits"] == 0 and sub_result["user_subscription_id"] > 0
        assert sql(f"SELECT count(*) FROM v3_commerce.subscriptions WHERE id={sub_result['user_subscription_id']} AND user_id={user_id} AND model_limits='{{\"acceptance-chat\":700}}'::jsonb") == "1"
        check("subscription redemption owner enforced", request("/api/commerce/redemptions/redeem", "POST", sub_body, token=other["access_token"]), 409)
        standard_pool = check("configure standard sealed inventory", request("/api/blind-box/admin/pools", "PUT", {**pool_body, "name": "standard-" + suffix, "scope": "standard", "daily_limit": 100}, token=token, origin=args.url))["data"]
        box_code = check("issue sealed box redemption", request("/api/redemption/", "POST", {"name": "boxes-" + suffix, "redeem_type": "blind_box", "blind_box_quantity": 3}, token=token, origin=args.url))["data"]
        box_body = {"key": box_code["key"]}
        box_result = check("redeem sealed box entitlement", request("/api/commerce/redemptions/redeem", "POST", box_body, token=token))["data"]
        box_replay = check("sealed box redemption replay", request("/api/commerce/redemptions/redeem", "POST", box_body, token=token))["data"]
        assert box_result == box_replay and box_result["redeem_type"] == "blind_box" and box_result["credits"] == 0 and box_result["blind_box_quantity"] == 3
        assert sql(f"SELECT count(*) FROM v3_marketplace.blind_box_items WHERE owner_user_id={user_id} AND status='available'") == "3"
        assert request("/api/billing/balance", token=token)[1]["data"]["balance_micro"] == 999902
        check("box redemption owner enforced", request("/api/commerce/redemptions/redeem", "POST", box_body, token=other["access_token"]), 409)
        check("invalid mixed redemption rejected", request("/api/redemption/", "POST", {"redeem_type": "subscription", "plan_id": plan["id"], "credits": 1}, token=token), 400)
        check("disable standard fixture pool", request("/api/blind-box/admin/pools", "PUT", {**standard_pool, "enabled": False}, token=token, origin=args.url))
        report = check("root funding economics report", request("/api/billing/funding-economics?day=2026-09-30", token=token))["data"]
        assert report["date"] == "2026-09-30" and isinstance(report["sources"], list)
        check("funding report rejects malformed day", request("/api/billing/funding-economics?day=2026-02-30", token=token), 400)
        check("funding report rejects malformed query encoding", request("/api/billing/funding-economics?day=%GG", token=token), 400)
        check("ordinary user cannot read procurement report", request("/api/billing/funding-economics", token=other["access_token"]), 403)
        sql(f"UPDATE v3_identity.users SET role='admin' WHERE id={other['user']['id']}")
        check("ordinary admin cannot read root procurement report", request("/api/billing/funding-economics", token=other["access_token"]), 403)
        sql(f"UPDATE v3_identity.users SET role='user' WHERE id={other['user']['id']}")
        check("API key cannot read procurement report", request("/api/billing/funding-economics", token=raw_key), 401)
        event_id = "audit-" + suffix
        other_id = other["user"]["id"]
        sql(f"INSERT INTO v3_security.security_audit_events(id,dedupe_key,source,decision,risk_code,severity,user_id,owner_user_id,token_id,review_status,prompt_preview,upstream_error_body,created_at) VALUES('{event_id}','{event_id}','gateway','deny','fixture','medium',{user_id},{other_id},9007199254740993,'unreviewed','private-fixture','private-fixture',now()),('{event_id}-foreign','{event_id}-foreign','gateway','deny','fixture','medium',{other_id},{user_id},1,'unreviewed','private-fixture','private-fixture',now())")
        owner_audit = check("retained audit owner read", request("/api/security-audit/events/" + event_id, token=other["access_token"]))
        assert owner_audit["id"] == event_id and owner_audit["token_id"] == "9007199254740993" and owner_audit["prompt_preview"] == "" and owner_audit["upstream_error_body"] == ""
        check("audit enduser does not gain owner scope", request("/api/security-audit/events/" + event_id + "-foreign", token=other["access_token"]), 404)
        check("admin audit alias requires admin", request("/api/marketplace/admin/security-audit/events", token=other["access_token"]), 403)
        check("API key cannot gain audit admin scope", request("/api/security-audit/events", token=raw_key), 401)
        owner_alias = check("legacy owner audit consumes retained records", request("/api/marketplace/security-audit/events", token=other["access_token"]))["data"]
        assert any(e["id"] == event_id for e in owner_alias["items"]) and "private-fixture" not in json.dumps(owner_alias)
        exported = check("legacy owner audit export redacts content", request("/api/marketplace/security-audit/events/export", token=other["access_token"]))
        assert event_id.encode() in exported and b"9007199254740993" in exported and b"private-fixture" not in exported
        reviewed = check("review retained string audit ID", request("/api/marketplace/admin/security-audit/events/" + event_id, "PATCH", {"review_status": "resolved", "review_note": "fixture reviewed"}, token=token, origin=args.url))["data"]
        assert reviewed["id"] == event_id and reviewed["review_status"] == "resolved"
        verify_live_status(request, check, sql, session, raw_key, chat, args.url)
        check("delete key", request(f"/api/token/{key_id}", "DELETE", token=token, origin=args.url))
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            rejected = request("/v1/models", token=raw_key)
            if rejected[0] == 401:
                break
            time.sleep(.1)
        check("deleted key invalidated at gateway", rejected, 401)
        check("delete IP scoped key", request(f"/api/token/{ip_key['id']}", "DELETE", token=token, origin=args.url))
        check("delete fixture channel", request(f"/api/catalog/channels/{channel_result['data']['id']}", "DELETE", token=token, origin=args.url))
        verify_restored(request, check, sql, session, other, suffix, args.url)
        verify_subscription_v2(request, check, sql, session, other, suffix, args.url)
        refreshed = check("refresh session", request("/api/user/refresh", "POST", {"refresh_token": session["refresh_token"]}))["data"]
        check("refresh token replay rejected", request("/api/user/refresh", "POST", {"refresh_token": session["refresh_token"]}), 401)
        check("logout", request("/api/user/logout", "POST", token=refreshed["access_token"]))
        check("revoked session rejected", request("/api/user/self", token=refreshed["access_token"]), 401)
        print(f"stack acceptance: {len(passed)} passed; external OAuth/passkey/payment providers are covered separately", flush=True)
    finally:
        fixture.shutdown()
        fixture.server_close()


if __name__ == "__main__":
    main()
