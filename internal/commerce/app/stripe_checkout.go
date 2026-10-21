package app

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/constant"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/shopspring/decimal"
	"github.com/stripe/stripe-go/v87"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Stored before contacting Stripe. Display preferences and later price changes
// must never change an existing order's denomination or original discount.
type stripeCheckoutSnapshot struct {
	Version   int    `json:"version"`
	Currency  string `json:"currency"`
	Amount    int64  `json:"amount"`
	Live      bool   `json:"live"`
	PropID    int    `json:"prop_id"`
	SessionID string `json:"session_id,omitempty"`
}

type stripePaymentEvidence struct {
	Checkout      *stripeCheckoutSnapshot `json:"stripe_checkout,omitempty"`
	ID            string                  `json:"id"`
	Reference     string                  `json:"client_reference_id"`
	Mode          string                  `json:"mode"`
	Status        string                  `json:"status"`
	PaymentStatus string                  `json:"payment_status"`
	Amount        int64                   `json:"amount_total"`
	Currency      string                  `json:"currency"`
	Live          bool                    `json:"livemode"`
	PaymentIntent string                  `json:"payment_intent"`
}

func snapshotStripeCheckout(payload *string, currency string, money float64, propID int) error {
	if math.IsNaN(money) || math.IsInf(money, 0) || money <= 0 || money >= float64(math.MaxInt64)/100 || StripeMoneyToMinorUnits(money) < 1 {
		return errors.New("invalid Stripe checkout amount")
	}
	envelope := map[string]any{}
	if *payload != "" && (json.Unmarshal([]byte(*payload), &envelope) != nil || envelope == nil) {
		return errors.New("invalid Stripe checkout payload")
	}
	envelope["stripe_checkout"] = stripeCheckoutSnapshot{Version: 1, Currency: currency, Amount: StripeMoneyToMinorUnits(money), Live: strings.HasPrefix(commercestore.StripeApiSecret, "sk_live_") || strings.HasPrefix(commercestore.StripeApiSecret, "rk_live_"), PropID: propID}
	data, err := json.Marshal(envelope)
	if err == nil {
		*payload = string(data)
	}
	return err
}

func snapshotStripeSubscriptionCheckout(order *commerceschema.SubscriptionOrder, currency string, propID int) error {
	if order.PaymentProvider != commerceschema.PaymentProviderStripe {
		return nil
	}
	base := order.Money
	switch normalizeSubscriptionCurrency(currency) {
	case "CNY":
	case "USD":
		if commercestore.USDExchangeRate <= 0 {
			return errors.New("invalid USD to CNY exchange rate")
		}
		base *= commercestore.USDExchangeRate
	default:
		return errors.New("unsupported Stripe subscription base currency")
	}
	currency = stripeOrderCurrency(order.ProviderPayload)
	charged, err := stripePriceFromCNY(base, currency)
	if err != nil {
		return err
	}
	order.Money = charged
	return snapshotStripeCheckout(&order.ProviderPayload, currency, charged, propID)
}

func stripePresentmentCurrency(currency string) (string, error) {
	currency = strings.ToLower(strings.TrimSpace(currency))
	if currency == "" {
		currency = "hkd"
	}
	if currency != "hkd" && currency != "cny" {
		return "", errors.New("unsupported Stripe payment currency")
	}
	return currency, nil
}

func stripeOrderCurrency(payload string) string {
	var data struct {
		Currency string `json:"stripe_currency"`
	}
	if json.Unmarshal([]byte(payload), &data) != nil {
		return ""
	}
	return data.Currency
}

func stripePriceFromCNY(base float64, currency string) (float64, error) {
	price := 0.0
	switch currency {
	case "hkd":
		price = commercestore.StripeHKDUnitPrice
	case "cny":
		price = commercestore.StripeCNYUnitPrice
	default:
		return 0, errors.New("unsupported Stripe payment currency")
	}
	if base <= 0 || math.IsNaN(base) || math.IsInf(base, 0) || price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
		return 0, errors.New("invalid Stripe currency price")
	}
	charged := decimal.NewFromFloat(base).Mul(decimal.NewFromFloat(price)).Round(2).InexactFloat64()
	if charged <= 0 || charged >= float64(math.MaxInt64)/100 {
		return 0, errors.New("invalid Stripe charged amount")
	}
	return charged, nil
}

func createStripeCheckout(ctx context.Context, reference, customer, email, product, currency string, money float64, successURL, cancelURL string, subscription bool) (string, error) {
	key := strings.TrimSpace(commercestore.StripeApiSecret)
	if !strings.HasPrefix(key, "sk_") && !strings.HasPrefix(key, "rk_") {
		return "", errors.New("invalid Stripe API key")
	}
	if currency != "cny" && currency != "usd" && currency != "hkd" {
		return "", errors.New("unsupported Stripe currency")
	}
	if math.IsNaN(money) || math.IsInf(money, 0) || money <= 0 || money >= float64(math.MaxInt64)/100 || StripeMoneyToMinorUnits(money) < 1 {
		return "", errors.New("invalid stripe amount")
	}
	// Stable across retries of the same immutable order; do not offer untracked
	// Stripe coupons on top of the platform's own funded discount snapshots.
	params := &stripe.CheckoutSessionCreateParams{
		ClientReferenceID: stripe.String(reference), SuccessURL: stripe.String(successURL), CancelURL: stripe.String(cancelURL),
		Mode:                  stripe.String("payment"),
		LineItems:             []*stripe.CheckoutSessionCreateLineItemParams{{Quantity: stripe.Int64(1), PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{Currency: stripe.String(currency), UnitAmount: stripe.Int64(StripeMoneyToMinorUnits(money)), ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{Name: stripe.String(product)}}}},
		Metadata:              map[string]string{"codego_order": reference},
		IntegrationIdentifier: stripe.String("codego_" + stripeIntegrationSuffix(reference)),
	}
	params.SetIdempotencyKey("codego-checkout:" + reference)
	if customer != "" {
		params.Customer = stripe.String(customer)
	} else {
		if email != "" {
			params.CustomerEmail = stripe.String(email)
		}
		params.CustomerCreation = stripe.String("always")
	}
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	result, err := stripe.NewClient(key).V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return "", err
	}
	if result.ID == "" || result.URL == "" {
		return "", errors.New("Stripe returned an incomplete Checkout Session")
	}
	if err := bindStripeCheckout(reference, subscription, result.ID); err != nil {
		return "", err
	}
	return result.URL, nil
}

func stripeCheckoutRejected(err error) bool {
	var apiError *stripe.Error
	return errors.As(err, &apiError) && apiError.HTTPStatusCode >= 400 && apiError.HTTPStatusCode < 500 && apiError.HTTPStatusCode != 409 && apiError.HTTPStatusCode != 429
}

func expireStripeCheckout(payload string) error {
	var original stripePaymentEvidence
	if json.Unmarshal([]byte(payload), &original) != nil || original.Checkout == nil || original.Checkout.SessionID == "" {
		return errors.New("Stripe checkout is not confirmed; wait for its expiry notification")
	}
	live := strings.HasPrefix(commercestore.StripeApiSecret, "sk_live_") || strings.HasPrefix(commercestore.StripeApiSecret, "rk_live_")
	if live != original.Checkout.Live {
		return errors.New("Stripe checkout environment mismatch")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	client := stripe.NewClient(commercestore.StripeApiSecret)
	session, err := client.V1CheckoutSessions.Expire(ctx, original.Checkout.SessionID, nil)
	if err != nil {
		if !stripeCheckoutRejected(err) {
			return err
		}
		session, err = client.V1CheckoutSessions.Retrieve(ctx, original.Checkout.SessionID, nil)
		if err != nil {
			return err
		}
	}
	if session == nil || session.ID != original.Checkout.SessionID || session.Livemode != live || session.Status != stripe.CheckoutSessionStatusExpired || session.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid {
		return errors.New("Stripe payment is already processing; wait for confirmation")
	}
	return nil
}

// Check expiry against the immutable Checkout Session before releasing a prop.
// An unrelated session in the same Stripe account must not cancel this order.
func ExpireStripeOrder(ctx context.Context, referenceID string, received map[string]any) error {
	if referenceID == "" {
		return nil
	}
	data, err := json.Marshal(received)
	if err != nil {
		return err
	}
	var session stripePaymentEvidence
	if json.Unmarshal(data, &session) != nil || session.Reference != referenceID || session.Mode != "payment" || !strings.HasPrefix(session.ID, "cs_") || session.PaymentStatus == "paid" {
		return errors.New("invalid Stripe expiry evidence")
	}
	LockOrder(referenceID)
	defer UnlockOrder(referenceID)
	return platformdb.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record any
		var payload, status, provider, propType string
		var money float64
		var order commerceschema.SubscriptionOrder
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", referenceID).First(&order).Error
		if err == nil {
			record, payload, status, provider, money, propType = &order, order.ProviderPayload, order.Status, order.PaymentProvider, order.Money, commerceschema.BlindBoxPropOrderTypeSubscription
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			var topup commerceschema.TopUp
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", referenceID).First(&topup).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}
			record, payload, status, provider, money, propType = &topup, topup.ProviderPayload, topup.Status, topup.PaymentProvider, topup.Money, commerceschema.BlindBoxPropOrderTypeTopup
		} else {
			return err
		}
		var original stripePaymentEvidence
		if provider != commerceschema.PaymentProviderStripe || json.Unmarshal([]byte(payload), &original) != nil || original.Checkout == nil {
			return errors.New("missing Stripe original checkout")
		}
		snapshot := original.Checkout
		if snapshot.Version != 1 || (snapshot.SessionID != "" && snapshot.SessionID != session.ID) || snapshot.Live != session.Live || snapshot.Currency != session.Currency || snapshot.Amount != session.Amount || session.Amount != StripeMoneyToMinorUnits(money) {
			return errors.New("Stripe expiry does not match original checkout")
		}
		if status != constant.TopUpStatusPending {
			return nil
		}
		if err := tx.Model(record).Updates(map[string]any{"status": constant.TopUpStatusExpired, "complete_time": platformruntime.GetTimestamp()}).Error; err != nil {
			return err
		}
		return ReleaseReservedBlindBoxPropByTradeNoTx(tx, referenceID, propType)
	})
}

func stripeIntegrationSuffix(reference string) string {
	// Deterministic letters keep the idempotent request identical on retry.
	var letters [8]byte
	for i := range letters {
		letters[i] = 'a' + reference[i%len(reference)]%26
	}
	return string(letters[:])
}

func bindStripeCheckout(reference string, subscription bool, sessionID string) error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var record any
		var payload *string
		if subscription {
			order := &commerceschema.SubscriptionOrder{}
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", reference).First(order).Error; err != nil {
				return err
			}
			record, payload = order, &order.ProviderPayload
		} else {
			order := &commerceschema.TopUp{}
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", reference).First(order).Error; err != nil {
				return err
			}
			record, payload = order, &order.ProviderPayload
		}
		var envelope map[string]json.RawMessage
		var snapshot stripeCheckoutSnapshot
		if json.Unmarshal([]byte(*payload), &envelope) != nil || json.Unmarshal(envelope["stripe_checkout"], &snapshot) != nil {
			return errors.New("missing Stripe checkout snapshot")
		}
		if snapshot.SessionID != "" && snapshot.SessionID != sessionID {
			return errors.New("Stripe session conflict")
		}
		snapshot.SessionID = sessionID
		data, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		envelope["stripe_checkout"] = data
		data, err = json.Marshal(envelope)
		if err != nil {
			return err
		}
		*payload = string(data)
		return tx.Model(record).Update("provider_payload", *payload).Error
	})
}

func validateStripePayment(original, received, reference string, money float64) (*stripePaymentEvidence, string, error) {
	var previous stripePaymentEvidence
	var paid stripePaymentEvidence
	if json.Unmarshal([]byte(original), &previous) != nil || previous.Checkout == nil || previous.Checkout.Version != 1 {
		return nil, "", errors.New("missing Stripe checkout evidence")
	}
	if json.Unmarshal([]byte(received), &paid) != nil || !strings.HasPrefix(paid.ID, "cs_") || paid.Reference != reference || paid.Mode != "payment" || paid.Status != "complete" || paid.PaymentStatus != "paid" || !strings.HasPrefix(paid.PaymentIntent, "pi_") {
		return nil, "", errors.New("invalid Stripe payment evidence")
	}
	snapshot := previous.Checkout
	if paid.Amount <= 0 || paid.Amount != snapshot.Amount || paid.Amount != StripeMoneyToMinorUnits(money) || paid.Currency != snapshot.Currency || paid.Live != snapshot.Live {
		return nil, "", errors.New("Stripe amount, currency or environment mismatch")
	}
	if (snapshot.SessionID != "" && snapshot.SessionID != paid.ID) || (previous.ID != "" && previous.ID != paid.ID) || (previous.PaymentIntent != "" && previous.PaymentIntent != paid.PaymentIntent) {
		return nil, "", errors.New("Stripe original payment mismatch")
	}
	snapshot.SessionID = paid.ID
	paid.Checkout = snapshot
	data, err := json.Marshal(paid)
	return &paid, string(data), err
}

func consumeStripeOriginalPropTx(tx *gorm.DB, userID int, tradeNo, orderType string, snapshot *stripeCheckoutSnapshot) error {
	if snapshot == nil || snapshot.PropID < 0 {
		return errors.New("missing Stripe original discount evidence")
	}
	var props []commerceschema.BlindBoxProp
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("reserved_order_trade_no = ? AND reserved_order_type = ? AND status = ?", tradeNo, orderType, commerceschema.BlindBoxPropStatusReserved).Limit(2).Find(&props).Error; err != nil {
		return err
	}
	if snapshot.PropID == 0 && len(props) == 0 {
		return nil
	}
	if len(props) != 1 || props[0].Id != snapshot.PropID || props[0].UserId != userID {
		return errors.New("Stripe original discount released or conflicting; manual review required")
	}
	return ConsumeReservedBlindBoxPropByTradeNoTx(tx, tradeNo, orderType)
}

func completeVerifiedStripeTopUp(reference, customer, received string) (*commerceschema.TopUp, int, error) {
	var completed *commerceschema.TopUp
	credited := 0
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var order commerceschema.TopUp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", reference).First(&order).Error; err != nil {
			return err
		}
		if order.PaymentProvider != commerceschema.PaymentProviderStripe || order.PaymentMethod != commerceschema.PaymentMethodStripe {
			return commerceschema.ErrPaymentMethodMismatch
		}
		paid, payload, err := validateStripePayment(order.ProviderPayload, received, reference, order.Money)
		if err != nil {
			return err
		}
		if order.ExternalPaymentID != "" && order.ExternalPaymentID != paid.PaymentIntent {
			return errors.New("Stripe PaymentIntent mismatch")
		}
		completed = &order
		if order.Status == constant.TopUpStatusSuccess {
			return nil
		}
		if (order.Status != constant.TopUpStatusPending && order.Status != constant.TopUpStatusExpired) || order.RefundStatus != "" || order.RefundNo != "" || order.RefundAmount != 0 || order.RefundQuota != 0 {
			return commerceschema.ErrTopUpStatusInvalid
		}
		var user identityschema.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", order.UserId).First(&user).Error; err != nil {
			return err
		}
		if err := consumeStripeOriginalPropTx(tx, order.UserId, reference, commerceschema.BlindBoxPropOrderTypeTopup, paid.Checkout); err != nil {
			return err
		}
		quota := calculateTopUpQuotaToAdd(&order)
		if quota <= 0 {
			return errors.New("invalid Stripe credited quota")
		}
		order.Status, order.CompleteTime = constant.TopUpStatusSuccess, platformruntime.GetTimestamp()
		order.ProviderPayload, order.ExternalPaymentID = payload, paid.PaymentIntent
		if err := tx.Save(&order).Error; err != nil {
			return err
		}
		if err := creditTopUpQuotaTx(tx, &order, quota, customer, ""); err != nil {
			return err
		}
		credited = quota
		return nil
	})
	if err == nil && credited > 0 {
		invalidateTopUpUserCache(completed.UserId)
	}
	return completed, credited, err
}
