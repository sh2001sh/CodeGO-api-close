import sys, json, time
from pathlib import Path
from batch_grant_blind_boxes import ApiClient, iter_records

client = ApiClient('http://127.0.0.1:3000')
login = client.request('POST', '/api/user/login', {'username': 'sh2001sh', 'password': sys.stdin.readline().strip()})
assert login.get('success'), login.get('message')
assert not login['data'].get('require_2fa'), 'Two-factor authentication required'
client.opener.addheaders = [('New-Api-User', str(login['data']['id']))]
print('Authenticated', flush=True)
ids = list(dict.fromkeys(Path('/root/requested-blind-box-users.txt').read_text(encoding='utf-8-sig').split()))
results = []
seen = set()
def get(path, query=None):
    r = client.request('GET', path, query=query)
    if not r.get('success'): raise RuntimeError(r.get('message'))
    return r.get('data')
for name in ids:
    row = {'identifier': name}
    try:
        data = get('/api/user/search', {'keyword': name})
        users = {int(u['id']): u for u in iter_records(data) if str(u.get('external_id', '')).upper() == name.upper() or u.get('username') == name or u.get('display_name') == name}
        if len(users) != 1:
            row['status'] = 'not_found' if not users else 'ambiguous'
        else:
            uid = next(iter(users)); row['user_id'] = uid
            if uid in seen:
                row['status'] = 'duplicate'
            else:
                seen.add(uid)
                grants = get(f'/api/blind-box/admin/users/{uid}/overview')['grants'] or []
                total = sum(int(g['quantity']) for g in grants)
                row['previous_quantity'] = total
                key = f'blind-box-grant:requested-20260905:{uid}'
                if any(g.get('idempotency_key') == key for g in grants):
                    row['status'] = 'already_processed'
                elif total >= 2:
                    row['status'] = 'skipped'
                else:
                    r = client.request('POST', f'/api/blind-box/admin/users/{uid}/grants', {'quantity': 1, 'reason': '指定名单补发：历史管理员发放数量少于2个', 'idempotency_key': key})
                    if not r.get('success'): raise RuntimeError(r.get('message'))
                    row['grant_id'] = r['data']['grant']['id']
                    check = get(f'/api/blind-box/admin/users/{uid}/overview')['grants']
                    assert any(g['id'] == row['grant_id'] and g['quantity'] == 1 for g in check), 'Verification failed'
                    row['status'] = 'granted'
    except Exception as e:
        row.update(status='failed', error=str(e))
    results.append(row)
    Path('/root/requested-blind-box-results.json').write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps(row, ensure_ascii=False), flush=True)
    time.sleep(1)
