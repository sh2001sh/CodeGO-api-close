# Subscription Renewal Bonus Audit - 2026-07-19

## Scope

The pre-fix renewal calculation used `completed_successful_orders + 1` even
though the current order had already been marked successful before
fulfillment. This advanced the second and third renewals by one tier.

Expected tiers:

| Successful purchase position | Reward rate |
| --- | --- |
| 2 | 3% |
| 3 | 5% |
| 4 and later | 8% |

## Audit Result

The production ledger contains five `subscription_bonus` credits. Four are
not same-plan renewal events and are excluded from this correction. Of eight
second-purchase monthly-plan candidates, one has a matched bonus credit and a
subscription snapshot that confirms the erroneous 8% grant.

### Confirmed Adjustment

| Field | Value |
| --- | --- |
| User ID | 115 |
| Subscription ID | 63 |
| Subscription order ID | 83 |
| Plan | Standard monthly plan |
| Original order completion | 2026-06-27 15:01:01 +08 |
| Bonus ledger credit | 2026-07-19 13:19:05 +08 |
| Applied bonus | 24,800,000 quota units ($49.60) |
| Correct second-purchase bonus | 9,300,000 quota units ($18.60) |
| Excess to reverse | 15,500,000 quota units ($31.00) |
| Current subscription total | 334,800,000 quota units |
| Current subscription used | 2,300,751 quota units |
| Current subscription remaining | 332,499,249 quota units |
| Current ledger available balance | 332,499,249 quota units |
| Post-adjustment total | 319,300,000 quota units |
| Post-adjustment remaining | 316,999,249 quota units |
| Post-adjustment ledger available balance | 316,999,249 quota units |

The subscription has enough unused balance for the full reversal. Its
`period_amount` is zero, so only the total subscription quota and matching
ledger balance require adjustment.

### Excluded Records

- Four other `subscription_bonus` ledger credits do not correspond to a second
  or third successful purchase of the same monthly plan. They are excluded.
- Seven other second-purchase monthly-plan candidates have no matched renewal
  bonus ledger credit or current subscription snapshot proving an excess.
- There are no successful-but-pending normal subscription orders at audit time.

## Required Adjustment Transaction

Run this once, in one database transaction, only after the renewal calculation
fix is deployed:

1. Lock subscription `63`, its billing account, and balance snapshot.
2. Re-read the subscription remaining balance and require at least
   `15,500,000` quota units.
3. Add one idempotent debit ledger entry with reason
   `subscription_bonus_correction`, reference type `subscription_order`, and
   reference ID `83`.
4. Decrease `user_subscriptions.amount_total` by `15,500,000` quota units.
5. Decrease the subscription ledger available balance by the same amount.
6. Record an audit log that includes the old reward, correct reward, and
   correction amount.
7. Verify the subscription remaining quota and ledger available balance both
   equal `316,999,249` quota units after the transaction.

## Release Gate

Do not apply the adjustment while the pre-fix image is serving purchases. The
renewal calculation fix and server-side preview must be released first so a
new second or third renewal cannot recreate the excess.

## Execution Result

- Executed at: 2026-07-19 14:30 CST, after production was upgraded to
  `v2.0.0-rc.33.9-alpha.32` and health checks passed.
- Idempotency key:
  `subscription-bonus-correction:subscription-order:83`.
- Debit ledger entry: `15,500,000` quota units with reason
  `subscription_bonus_correction` and reference `subscription_order:83`.
- Subscription `63` amount total: `334,800,000` to `319,300,000` quota units.
- `period_amount` was unchanged at `0`.
- Ledger available balance after the transaction: `314,859,061` quota units.
  This is below the audit-time projection because normal subscription usage
  consumed `2,140,188` additional units before the transaction; the debit and
  the subscription total both match the intended correction.
