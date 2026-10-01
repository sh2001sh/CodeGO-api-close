package commerce

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

var (
	ErrWalletSelf             = errors.New("不能向自己转账")
	ErrWalletPasswordNotSet   = errors.New("请先设置支付密码")
	ErrWalletPasswordWrong    = errors.New("支付密码错误")
	ErrWalletPasswordLocked   = errors.New("支付密码已临时锁定")
	ErrWalletAccountPassword  = errors.New("当前登录密码错误")
	ErrWalletInsufficient     = errors.New("通用额度余额不足")
	ErrWalletRewardLocked     = errors.New("盲盒奖励额度尚未完全解锁转账")
	ErrWalletEmailUnavailable = errors.New("支付密码邮箱恢复尚未配置")
)

const (
	walletMinimum     = credits.Micro(credits.PerCredit / 100)
	walletFeeBPS      = 100
	walletMaxFailures = 5
)

type WalletTransferInput struct {
	RecipientExternalID string        `json:"recipient_external_id"`
	Amount              credits.Micro `json:"amount_micro"`
	PaymentPassword     string        `json:"payment_password"`
	RequestID           string        `json:"request_id"`
}

type WalletPasswordInput struct {
	VerificationMethod string `json:"verification_method"`
	CurrentPassword    string `json:"current_password"`
	OldPaymentPassword string `json:"old_payment_password"`
	NewPaymentPassword string `json:"new_payment_password"`
	ConfirmPassword    string `json:"confirm_password"`
	EmailCode          string `json:"email_code"`
}

type WalletRecipient struct {
	ExternalID        string `json:"external_id"`
	DisplayNameMasked string `json:"display_name_masked"`
}

type WalletSecurity struct {
	PasswordSet               bool   `json:"password_set"`
	LockedUntil               int64  `json:"locked_until"`
	RemainingPasswordAttempts int    `json:"remaining_password_attempts"`
	RequiresAccountPassword   bool   `json:"requires_account_password"`
	EmailBound                bool   `json:"email_bound"`
	EmailMasked               string `json:"email_masked"`
	EmailRecoveryAvailable    bool   `json:"email_recovery_available"`
}

type WalletTransferItem struct {
	ID                      int64         `json:"id"`
	RequestID               string        `json:"request_id"`
	Direction               string        `json:"direction"`
	CounterpartyExternalID  string        `json:"counterparty_external_id"`
	CounterpartyDisplayName string        `json:"counterparty_display_name_masked"`
	Amount                  credits.Micro `json:"amount_micro"`
	Fee                     credits.Micro `json:"fee_micro"`
	TotalDebit              credits.Micro `json:"total_debit_micro"`
	BalanceAfter            credits.Micro `json:"balance_after"`
	Status                  string        `json:"status"`
	CreatedAt               int64         `json:"created_at"`
}

type WalletHistory struct {
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
	Total    int64                `json:"total"`
	Items    []WalletTransferItem `json:"items"`
}

type WalletOverview struct {
	MicroPerCredit int64          `json:"micro_per_credit"`
	MinMicro       credits.Micro  `json:"min_micro"`
	Balance        credits.Micro  `json:"balance"`
	Transferable   credits.Micro  `json:"transferable_balance"`
	RewardLocked   credits.Micro  `json:"reward_locked_balance"`
	FeeBPS         int            `json:"fee_bps"`
	Security       WalletSecurity `json:"security"`
	History        WalletHistory  `json:"history"`
}

func validWalletExternalID(id string) bool {
	if len(id) != 6 {
		return false
	}
	for _, c := range id {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func validWalletPassword(password string) bool {
	if len(password) > 72 || !utf8.ValidString(password) || password != strings.TrimSpace(password) {
		return false
	}
	length := utf8.RuneCountInString(password)
	if length < 8 || length > 64 {
		return false
	}
	var letter, number bool
	for _, c := range password {
		letter = letter || unicode.IsLetter(c)
		number = number || unicode.IsNumber(c)
	}
	return letter && number
}

// Division precedes addition so a large amount cannot overflow fee arithmetic.
func walletFee(amount credits.Micro) credits.Micro {
	fee := amount / 100
	if amount%100 != 0 {
		fee++
	}
	return fee
}

func maskWalletName(value string) string {
	r := []rune(strings.TrimSpace(value))
	if len(r) == 0 {
		return "***"
	}
	if len(r) == 1 {
		return string(r[0]) + "***"
	}
	if len(r) == 2 {
		return string(r[0]) + "*"
	}
	count := min(len(r)-2, 4)
	return string(r[0]) + strings.Repeat("*", count) + string(r[len(r)-1])
}

func maskWalletEmail(email string) string {
	p := strings.Split(email, "@")
	if len(p) != 2 || p[0] == "" {
		return ""
	}
	r := []rune(p[0])
	visible := string(r[0])
	if len(r) > 2 {
		visible += string(r[len(r)-1])
	}
	return visible + "***@" + p[1]
}
