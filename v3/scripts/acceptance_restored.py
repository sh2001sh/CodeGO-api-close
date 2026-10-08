"""Restored features exercised by acceptance-stack's isolated public endpoint."""

import json
import re
import time


def verify_live_status(request, check, sql, root, key, chat, origin):
    user = int(root["user"]["id"])
    success = request("/v1/chat/completions", "POST", chat, token=key)
    check("live status successful request", success)
    failure = request("/v1/chat/completions", "POST", {**chat, "messages": [{"role": "user", "content": "force-error"}]}, token=key)
    assert failure[0] in (502, 503), "real upstream failure must remain a failure"
    ids = [result[2].get('X-Request-Id', '') for result in (success, failure)]
    assert all(re.fullmatch(r'req_[a-f0-9]+', value) for value in ids) and ids[0] != ids[1]
    where = f"user_id={user} AND model='acceptance-chat' AND request_id IN ('{ids[0]}','{ids[1]}')"
    deadline = time.monotonic() + 8
    while time.monotonic() < deadline:
        facts = sql(f"SELECT count(*),count(*) FILTER(WHERE status='success'),count(*) FILTER(WHERE status='failed' AND counted_in_success_rate) FROM v3_audit.request_audits WHERE {where}")
        total, succeeded, failed = map(int, facts.split('|'))
        if total == 2 and succeeded == 1 and failed == 1:
            break
        time.sleep(.1)
    else:
        raise AssertionError("real gateway success/failure summaries were not persisted")
    assert sql(f"SELECT amount FROM v3_audit.request_audits WHERE request_id='{ids[0]}'") == '8'
    assert sql(f"SELECT amount FROM v3_audit.request_audits WHERE request_id='{ids[1]}'") == '0'
    background = check("durable background response accepted", request("/v1/responses", "POST", {"model": "acceptance-chat", "input": "hello", "background": True}, token=key))
    job = background['id']
    assert re.fullmatch(r'resp_bg_[a-f0-9]+', job)
    deadline = time.monotonic() + 25
    while time.monotonic() < deadline:
        completed = request(f"/v1/responses/{job}", token=key)
        if completed[0] == 200 and completed[1].get('status') == 'completed':
            facts = sql(f"SELECT status,amount,counted_in_success_rate FROM v3_audit.request_audits WHERE request_id='{job}'")
            usage = sql(f"SELECT sum(amount) FROM v3_billing.usage_logs WHERE request_id='{job}'")
            if facts == 'success|8|t' and usage == '8':
                break
        time.sleep(.2)
    else:
        raise AssertionError("worker background completion, actual charge and desktop summary did not converge")
    check("worker background completion recorded once with actual charge", completed)
    assert sql(f"SELECT count(*) FROM v3_audit.request_audits WHERE request_id='{job}'") == '1'
    token = root["access_token"]
    started = check("desktop live status authorization starts", request("/api/desktop/auth/session", "POST", {"device_name": "live-status-fixture", "platform": "test", "app_version": "restored"}, origin=origin))["data"]
    check("desktop live status authorization approved", request("/api/desktop/auth/approve", "POST", {"session_id": started["session_id"]}, token=token, origin=origin))
    grant = check("desktop live status device granted", request("/api/desktop/auth/poll", "POST", {"session_id": started["session_id"]}, origin=origin))["data"]
    groups = check("desktop status reflects real gateway requests", request("/api/desktop/group-status", token=grant["access_token"]))["data"]
    models = [m for group in groups if group["group"] == 'default' for m in group["models"] if m["model"] == 'acceptance-chat']
    assert len(models) == 1 and models[0]["status"] != 'unknown'
    assert models[0]["request_count"] >= 2 and 0 < models[0]["success_rate"] < 100
    assert len(models[0]["series"]) == 12 and any(bucket["request_count"] >= 2 for bucket in models[0]["series"])
    devices = check("desktop live status device listed", request("/api/desktop/devices", token=token))["data"]
    device = next(d for d in devices if d["device_name"] == 'live-status-fixture' and d["revoked_at"] == 0)
    check("desktop live status device revoked", request(f"/api/desktop/devices/{device['id']}", "DELETE", token=token, origin=origin))


def verify_restored(request, check, sql, root, other, suffix, origin):
    token, user = root["access_token"], root["user"]["id"]
    other_token, other_user = other["access_token"], other["user"]["id"]
    check("email verification explicitly unavailable without SMTP", request("/api/verification?email=fixture%40example.test"), 503)
    check("password recovery explicitly unavailable without SMTP", request("/api/reset_password?email=fixture%40example.test"), 503)
    check("invalid reset proof refused", request("/api/user/reset", "POST", {"email": "fixture@example.test", "token": "invalid", "password": "fixture-invalid-proof"}), 401)
    check("root performance tools registered", request("/api/performance/stats", token=token))
    check("ordinary user cannot access performance tools", request("/api/performance/stats", token=other_token), 403)
    check("absent native request disk cache is explicit", request("/api/performance/disk_cache", "DELETE", token=token), 409)
    logs = check("native service file logs available", request("/api/performance/logs", token=token))["data"]
    assert logs["enabled"] and logs["output"] == "stderr,file"
    active = {f["name"] for f in logs["files"] if f["name"].endswith(".active.log")}
    assert len(active) >= 3, "all three services must have actual active logs"
    check("retention tool preserves active files", request("/api/performance/logs?mode=by_count&value=1", "DELETE", token=token))
    retained = check("active files remain after cleanup", request("/api/performance/logs", token=token))["data"]
    assert active <= {f["name"] for f in retained["files"]}
    check("ordinary user cannot alter root settings", request("/api/settings", token=other_token), 403)
    check("missing deployment credentials fail explicitly", request("/api/deployments/", token=token), 503)
    model = int(sql(f"INSERT INTO v3_catalog.models(model_name) VALUES('restored-{suffix}') RETURNING id").splitlines()[0])
    favorite = {"model_id": model, "favorite": True}
    check("model favorite save", request("/api/models/favorites", "PUT", favorite, token=token))
    result = check("model favorite idempotent save", request("/api/models/favorites", "PUT", favorite, token=token))["data"]
    assert result["model_ids"] == [model]
    isolated = check("model favorites are owner scoped", request("/api/models/favorites", token=other_token))["data"]
    assert model not in isolated["model_ids"]
    affiliate = check("invitation code allocated", request("/api/user/aff", token=token))["data"]
    assert affiliate and affiliate == request("/api/user/aff", token=token)[1]["data"]
    policy_version = check("restored registration policy version", request("/api/policies/current"))["data"]["version"]
    agreements = {"accepted_terms_version": policy_version, "accepted_privacy_version": policy_version, "agreement_locale": "en"}
    invited = check("new registration binds inviter", request("/api/user/register", "POST", {"username": "invited_" + suffix, "password": "fixture-password-" + suffix, "aff_code": affiliate, **agreements}, origin=origin))["data"]
    assert sql(f"SELECT inviter_id FROM v3_identity.users WHERE id={invited['user']['id']}") == str(user)
    unattributed = check("invalid invitation creates no false attribution", request("/api/user/register", "POST", {"username": "badref_" + suffix, "password": "fixture-password-" + suffix, "aff_code": "unknown-fixture-code", **agreements}))["data"]
    assert sql(f"SELECT inviter_id IS NULL FROM v3_identity.users WHERE id={unattributed['user']['id']}") == "t"
    overview = check("invitation reward overview", request("/api/user/aff/rewards", token=token))["data"]
    retained = check("retained invitation overview alias", request("/api/user/aff/overview", token=token))["data"]
    assert overview == retained
    assert overview["affiliate_code"] == affiliate and overview["invited_count"] >= 1
    check("removed lucky participation returns not found", request("/api/daily-lucky-number/self", token=other_token), 404)
    check("removed lucky configuration returns not found", request("/api/daily-lucky-number/admin/config", token=token), 404)
    check("removed lucky backfill returns not found", request("/api/daily-lucky-number/admin/backfill", "POST", {}, token=token), 404)
    plan = check("legacy monthly package can still be configured", request("/api/subscription/admin/plans", "POST", {"name": "lucky-" + suffix, "currency": "usd", "price_minor": 100, "credits": 5000, "duration_unit": "month", "duration_value": 1, "plan_type": "monthly", "reset_period": "never", "enabled": True, "lucky_draw_enabled": True, "membership_tier": "pro"}, token=token, origin=origin))["data"]
    assert plan["lucky_draw_enabled"]
    granted = check("administrative monthly package grant", request(f"/api/subscription/admin/users/{other_user}/subscriptions", "POST", {"plan_id": plan["id"], "request_id": "lucky-grant-" + suffix}, token=token, origin=origin))["data"]
    replay = check("monthly package grant replay", request(f"/api/subscription/admin/users/{other_user}/subscriptions", "POST", {"plan_id": plan["id"], "request_id": "lucky-grant-" + suffix}, token=token))["data"]
    assert granted == replay
    assert sql(f"SELECT count(*) FROM v3_commerce.subscription_lucky_numbers WHERE user_id={other_user}") == "0"
    check("reset reward without opportunity refused", request("/api/subscription/self/reset-opportunity/use", "POST", {}, token=other_token), 409)
    # Seed an imported opportunity in the selected isolated fixture, then use
    # the actual account-rotation handler and verify monthly replay denial.
    sql(f"INSERT INTO v3_commerce.subscription_reset_opportunity_accounts(user_id,available_total,earned_total) VALUES({other_user},2,2)")
    reset = check("reset reward rotates real subscription funding", request("/api/subscription/self/reset-opportunity/use", "POST", {}, token=other_token))["data"]
    assert reset["reset_opportunity"]["available_count"] == 1
    check("second reset in same month refused", request("/api/subscription/self/reset-opportunity/use", "POST", {}, token=other_token), 409)
    summary = check("reset opportunity remains spent once", request("/api/subscription/self/reset-opportunity", token=other_token))["data"]
    assert summary["available_count"] == 1 and summary["used_total"] == 1
    sql(f"UPDATE v3_identity.users SET role='admin' WHERE id={other_user}")
    check("ordinary administrator cannot alter root settings", request("/api/settings", token=other_token), 403)
    check("ordinary administrator cannot access performance tools", request("/api/performance/stats", token=other_token), 403)
    sql(f"UPDATE v3_identity.users SET role='user' WHERE id={other_user}")
