package commerce

import (
	"context"
	"strings"
	"unicode"
)

// CheckoutSelection fixes the buyer's nonmonetary cashier choice. Price,
// currency, credits and credentials remain the server's order snapshot.
type CheckoutSelection struct {
	PaymentMethod string `json:"payment_method,omitempty"`
	PayCurrency   string `json:"pay_currency,omitempty"`
	PayMethodType string `json:"pay_method_type,omitempty"`
	PayMethodName string `json:"pay_method_name,omitempty"`
}

func (v CheckoutSelection) validFor(provider string) bool {
	for _, field := range []string{v.PaymentMethod, v.PayCurrency, v.PayMethodType, v.PayMethodName} {
		if len(field) > 64 || strings.TrimSpace(field) != field {
			return false
		}
		for _, r := range field {
			if unicode.IsControl(r) {
				return false
			}
		}
	}
	method := v.PaymentMethod
	if method == provider || (provider == "waffo_pancake" && method == "waffo-pancake") {
		method = ""
	}
	if provider != "epay" && method != "" {
		return false
	}
	if method != "" && !paymentToken(method) {
		return false
	}
	if v.PayCurrency != "" && (provider != "nowpayments" || !paymentToken(v.PayCurrency)) {
		return false
	}
	return (v.PayMethodType == "" && v.PayMethodName == "") || provider == "waffo"
}

func paymentToken(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func (s *Service) checkout(ctx context.Context, o Order, success, cancel string) (Checkout, error) {
	p := s.providers[o.Provider]
	if p == nil {
		return Checkout{}, ErrProviderUnavailable
	}
	v := o.Selection
	if !v.validFor(o.Provider) {
		return Checkout{}, ErrInvalid
	}
	// Clone the adapter, never mutate the shared configured provider while two
	// buyers select different cashiers concurrently.
	switch original := p.(type) {
	case *Epay:
		copy := *original
		if v.PaymentMethod != "" && v.PaymentMethod != "epay" {
			copy.cfg.PaymentType = v.PaymentMethod
		}
		p = &copy
	case *NowPayments:
		copy := *original
		if v.PayCurrency != "" {
			copy.cfg.PayCurrency = v.PayCurrency
		}
		p = &copy
	case *Waffo:
		copy := *original
		if v.PayMethodType != "" {
			copy.cfg.PayMethodType = v.PayMethodType
		}
		if v.PayMethodName != "" {
			copy.cfg.PayMethodName = v.PayMethodName
		}
		p = &copy
	default:
		if v.PayCurrency != "" || v.PayMethodType != "" || v.PayMethodName != "" || (v.PaymentMethod != "" && v.PaymentMethod != o.Provider && v.PaymentMethod != "waffo-pancake") {
			return Checkout{}, ErrInvalid
		}
	}
	return p.Checkout(ctx, o, success, cancel)
}
