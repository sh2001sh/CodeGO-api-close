"""Subscription redesign through the isolated public stack and actual worker."""

import re
import time


def verify_subscription_v2(request, check, sql, root, other, suffix, origin):
    token, user = root['access_token'], root['user']['id']
    other_token = other['access_token']
    rules_path = '/api/subscription/admin/redesign-rules'
    quote_path = '/api/subscription/self/wallet-conversion/quote'
    confirm_path = '/api/subscription/self/wallet-conversion/confirm'
    check('anonymous redesign rules refused', request(rules_path), 401)
    check('ordinary user cannot configure conversion', request(rules_path, token=other_token), 403)
    policy = check('migration stops new refresh rewards before funded campaign activation',
        request('/api/subscription/admin/referral-policy', token=token))['data']
    assert policy['effective_at'] is not None and policy['enabled'] is False and policy['total_budget_credits'] == 0
    plan = check('legacy entitlement remains configurable', request('/api/subscription/admin/plans', 'POST', {
        'name': 'v2-legacy-' + suffix, 'price_minor': 100, 'currency': 'usd', 'credits': 1000,
        'duration_unit': 'day', 'duration_value': 30, 'reset_period': 'never', 'enabled': True,
        'policy_version': 'legacy'}, token=token, origin=origin))['data']
    def bind(key):
        return check('grant independent legacy conversion fixture', request('/api/subscription/admin/bind', 'POST',
            {'user_id': user, 'plan_id': plan['id'], 'request_id': key + suffix}, token=token, origin=origin))['data']['id']
    expired, valid = bind('expired-'), bind('valid-')
    quote = check('unreviewed conversion has no executable quote', request(quote_path, 'POST',
        {'subscription_id': expired}, token=token))['data']
    assert quote['state'] == 'needs_review' and not quote['quote_id']
    rules = check('review original-version conversion ratio', request(rules_path, 'PUT', {'conversion_rules': [{
        'plan_id': plan['id'], 'basis_key': quote['basis_key'], 'source_credits': 1000,
        'wallet_credits': 100000, 'paid_wallet_credits': 0, 'enabled': True, 'reviewed': True,
        'note': 'audited fixture grant, reward only'}], 'card_rules': []}, token=token, origin=origin))['data']
    expired_quote = check('quote before expiry', request(quote_path, 'POST', {'subscription_id': expired}, token=token))['data']
    assert re.fullmatch(r'v3_[a-f0-9]+', expired_quote['quote_id'])
    sql(f'UPDATE v3_commerce.subscriptions SET expires_at=now() WHERE id={expired}')
    before = int(request('/api/billing/balance', token=token)[1]['data']['balance_micro'])
    check('expiry after quote refuses confirmation', request(confirm_path, 'POST', {
        'quote_id': expired_quote['quote_id'], 'request_id': 'expired-confirm-' + suffix,
        'accepted_terms': True}, token=token), 409)
    check('expired package cannot quote', request(quote_path, 'POST', {'subscription_id': expired}, token=token), 409)
    assert int(request('/api/billing/balance', token=token)[1]['data']['balance_micro']) == before
    quote = check('valid legacy quote freezes audited ratio', request(quote_path, 'POST', {'subscription_id': valid}, token=token))['data']
    assert quote['state'] == 'quoted' and quote['target_credits'] == 100000 and quote['paid_credits'] == 0
    body = {'quote_id': quote['quote_id'], 'request_id': 'whole-' + suffix, 'accepted_terms': True}
    check('conversion quote cannot be stolen', request(confirm_path, 'POST', body, token=other_token), 404)
    no_consent = {**body, 'accepted_terms': False}
    check('whole conversion requires consent', request(confirm_path, 'POST', no_consent, token=token), 400)
    result = request(confirm_path, 'POST', body, token=token)
    assert result[0] in (200, 202), result
    receipt_path = '/api/subscription/self/wallet-conversion/' + body['request_id']
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        receipt = request(receipt_path, token=token)
        if receipt[0] == 200 and receipt[1]['data']['state'] == 'completed':
            break
        # The same authorized request resumes after committed balance delivery.
        result = request(confirm_path, 'POST', body, token=token)
        assert result[0] in (200, 202), result
        time.sleep(.1)
    else:
        raise AssertionError('whole conversion did not settle through real Redis/outbox')
    check('whole conversion settles through real worker', receipt)
    check('completed conversion replays same receipt', request(confirm_path, 'POST', body, token=token))
    assert int(request('/api/billing/balance', token=token)[1]['data']['balance_micro']) == before + 100000
    assert sql(f"SELECT state,benefits_until=expires_at,next_reset_at IS NULL FROM v3_commerce.subscriptions WHERE id={valid}") == 'canceled|t|t'
    assert sql(f"SELECT sum(remaining_amount) FROM v3_billing.funding_lots WHERE source='subscription_conversion' AND metadata->>'subscription_id'='{valid}' AND non_transferable AND non_refundable") == '100000'
    check('conversion receipt owner enforced', request(receipt_path, token=other_token), 404)
    new_plan = check('standard package fixed total configuration', request('/api/subscription/admin/plans', 'POST', {
        'name': 'v2-fixed-' + suffix, 'price_minor': 100, 'currency': 'usd', 'credits': 20000,
        'duration_unit': 'day', 'duration_value': 90, 'reset_period': 'never', 'enabled': True,
        'policy_version': 'standard_v2'}, token=token, origin=origin))['data']
    sql(f'INSERT INTO v3_commerce.subscription_reset_opportunity_accounts(user_id,earned_total,available_total) VALUES({user},3,3)')
    rules = check('review limited reset-card compensation budget', request(rules_path, 'PUT', {'conversion_rules': [], 'card_rules': [{
        'name': 'bound-card-' + suffix, 'reference_plan_id': plan['id'], 'card_plan_id': new_plan['id'],
        'credits': 20000, 'cost_per_card': 700, 'baseline_cost': 100, 'budget_total': 2100,
        'incremental_budget_total': 1800, 'enabled': True, 'reviewed': True, 'note': 'isolated compensation fixture'}]}, token=token, origin=origin))['data']
    rule = next(r for r in rules['card_rules'] if r['reference_plan_id'] == plan['id'])
    card_quote = check('voluntary reset-card quantity quote', request('/api/subscription/self/reset-cards/quote', 'POST', {
        'rule_id': rule['id'], 'quantity': 2}, token=token))['data']
    exchange_body = {'quote_id': card_quote['quote_id'], 'request_id': 'cards-' + suffix, 'accepted_terms': True}
    exchange = check('exchange exactly selected reset opportunities', request('/api/subscription/self/reset-cards/confirm', 'POST', exchange_body, token=token))['data']
    check('reset-card exchange replay', request('/api/subscription/self/reset-cards/confirm', 'POST', exchange_body, token=token))
    assert exchange['quantity'] == 2 and exchange['remaining_count'] == 1 and len(exchange['cards']) == 2
    assert sql(f'SELECT earned_total,used_total,exchanged_total,available_total FROM v3_commerce.subscription_reset_opportunity_accounts WHERE user_id={user}') == '3|0|2|1'
    card = exchange['cards'][0]
    activation_path = f"/api/subscription/self/reset-cards/{card['id']}/activate"
    activation_body = {'request_id': 'activate-' + suffix}
    check('bound card owner enforced', request(activation_path, 'POST', activation_body, token=other_token), 404)
    activated = check('bound card activates independent fixed subscription', request(activation_path, 'POST', activation_body, token=token))['data']
    replay = check('bound card single activation replay', request(activation_path, 'POST', activation_body, token=token))['data']
    assert activated == replay and activated['state'] == 'activated', (activated, replay)
    sub = int(activated['subscription_id'])
    assert sql(f'SELECT policy_version,total_credits,reset_period,next_reset_at IS NULL FROM v3_commerce.subscriptions WHERE id={sub}') == 'standard_v2|20000|never|t'
    check('standard package administrative reset denied', request(f'/api/subscription/admin/user_subscriptions/{sub}/reset', 'POST', {'request_id': 'no-reset-' + suffix}, token=token), 409)
    check('consumption referral summary has permanent owner-only terms', request('/api/user/aff/consumption-rewards', token=token))
