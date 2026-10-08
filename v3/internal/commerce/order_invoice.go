package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var (
	ErrInvoiceDetailsRequired       = errors.New("commerce: invoice purchaser details required")
	ErrInvoiceDetailsConflict       = errors.New("commerce: invoice purchaser details already fixed")
	ErrInvoiceSellerMissing         = errors.New("commerce: invoice seller address unavailable")
	ErrInvoiceCharactersUnsupported = errors.New("commerce: invoice text contains unsupported characters")
)

// OrderInvoice is an immutable payment document. Corrections and refunds create
// linked documents; ownership is checked on every download.
type OrderInvoice struct {
	Number string
	PDF    []byte
}

type IssueOrderInvoiceInput struct {
	BuyerName    string `json:"buyer_name"`
	BuyerAddress string `json:"buyer_address"`
	BuyerCountry string `json:"buyer_country,omitempty"`
	BuyerTaxID   string `json:"buyer_tax_id,omitempty"`
}

type orderInvoiceData struct {
	number, buyer, buyerAddress, sellerAddress, description, trade, provider, currency, amount string
	sellerBRN, paymentReference, website, buyerCountry, buyerTaxID                             string
	documentType, relatedNumber, correctionReason                                              string
	details                                                                                    []string
	paid, issued                                                                               time.Time
}

func (s *Service) DownloadOrderInvoice(ctx context.Context, userID int64, trade string) (OrderInvoice, error) {
	return s.orderInvoiceDocument(ctx, userID, trade, nil)
}

// IssueOrderInvoice serializes against the order lock also used by refunds.
// Identical retries return the exact saved PDF; buyer edits require an explicit correction.
func (s *Service) IssueOrderInvoice(ctx context.Context, userID int64, trade string, input IssueOrderInvoiceInput) (OrderInvoice, error) {
	input = normalizeInvoiceInput(input)
	if !validInvoiceInput(input) {
		return OrderInvoice{}, ErrInvalid
	}
	return s.orderInvoiceDocument(ctx, userID, trade, &input)
}

func normalizeInvoiceInput(input IssueOrderInvoiceInput) IssueOrderInvoiceInput {
	input.BuyerName = strings.TrimSpace(input.BuyerName)
	input.BuyerAddress = strings.ReplaceAll(strings.TrimSpace(input.BuyerAddress), "\r\n", "\n")
	input.BuyerCountry, input.BuyerTaxID = strings.TrimSpace(input.BuyerCountry), strings.TrimSpace(input.BuyerTaxID)
	return input
}

func validInvoiceInput(input IssueOrderInvoiceInput) bool {
	return invoiceTextValid(input.BuyerName, 200, false) && invoiceTextValid(input.BuyerAddress, 600, true) &&
		(input.BuyerCountry == "" || invoiceTextValid(input.BuyerCountry, 100, false)) &&
		(input.BuyerTaxID == "" || invoiceTextValid(input.BuyerTaxID, 64, false))
}

func invoiceTextValid(value string, limit int, multiline bool) bool {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit {
		return false
	}
	return strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) && (!multiline || r != '\n') }) < 0
}

func (s *Service) orderInvoiceDocument(ctx context.Context, userID int64, trade string, input *IssueOrderInvoiceInput) (OrderInvoice, error) {
	if userID <= 0 || strings.TrimSpace(trade) == "" || len(trade) > 256 {
		return OrderInvoice{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return OrderInvoice{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	o, err := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE user_id=$1 AND trade_no=$2 FOR UPDATE`, userID, trade))
	if errors.Is(err, pgx.ErrNoRows) {
		return OrderInvoice{}, ErrNotFound
	}
	if err != nil {
		return OrderInvoice{}, err
	}
	if (o.State != "paid" && o.State != "refunded") || o.PaidAt == nil || o.AmountMinor <= 0 {
		return OrderInvoice{}, ErrStateConflict
	}
	stored, err := loadLatestOrderInvoice(ctx, tx, o.ID)
	result := stored.OrderInvoice
	if err == nil {
		if input != nil && stored.input != *input {
			return OrderInvoice{}, ErrInvoiceDetailsConflict
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		if input == nil {
			return OrderInvoice{}, ErrInvoiceDetailsRequired
		}
		result, err = s.insertOrderInvoice(ctx, tx, o, *input)
	}
	if err != nil {
		return OrderInvoice{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return OrderInvoice{}, err
	}
	return result, nil
}

func (s *Service) insertOrderInvoice(ctx context.Context, tx pgx.Tx, o Order, input IssueOrderInvoiceInput) (OrderInvoice, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT value FROM v3_platform.settings WHERE key='InvoiceSellerAddress' AND NOT sensitive`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrderInvoice{}, ErrInvoiceSellerMissing
	}
	if err != nil {
		return OrderInvoice{}, err
	}
	var address string
	if json.Unmarshal(raw, &address) != nil {
		return OrderInvoice{}, ErrInvoiceSellerMissing
	}
	address = strings.ReplaceAll(strings.TrimSpace(address), "\r\n", "\n")
	if !invoiceTextValid(address, 600, true) {
		return OrderInvoice{}, ErrInvoiceSellerMissing
	}
	issued, paid := s.cfg.Now().UTC(), o.PaidAt.UTC()
	number := fmt.Sprintf("CG-%04d-%012d", issued.In(time.FixedZone("HKT", 28800)).Year(), o.ID)
	description := "API account credit"
	switch o.Kind {
	case "subscription":
		description = "AI subscription"
		if o.PlanSnapshot.Name != "" {
			description += " / " + o.PlanSnapshot.Name
		}
	case "fuel":
		description = "Subscription additional credit"
	}
	data := orderInvoiceData{number: number, buyer: input.BuyerName, buyerAddress: input.BuyerAddress,
		sellerAddress: strings.TrimSpace(address), description: description, trade: o.TradeNo, provider: o.Provider,
		currency: strings.ToUpper(o.Currency), amount: formatCurrencyMinor(o.AmountMinor, o.Currency), issued: issued, paid: paid}
	data.sellerBRN, data.documentType = "81318858", "invoice"
	data.buyerCountry, data.buyerTaxID = input.BuyerCountry, input.BuyerTaxID
	if o.ProviderReference != nil {
		data.paymentReference = *o.ProviderReference
	}
	data.details = orderInvoiceDetails(o)
	pdf, err := renderOrderInvoice(data)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			err = errors.Join(ErrInvoiceCharactersUnsupported, err)
		}
		return OrderInvoice{}, err
	}
	snapshot, err := json.Marshal(map[string]any{"seller_name": "CodeGo AI Limited", "seller_name_zh": "碼高智能有限公司",
		"seller_brn": data.sellerBRN, "description": description, "trade_no": o.TradeNo, "provider": o.Provider,
		"payment_reference": data.paymentReference, "amount_minor": o.AmountMinor, "currency": o.Currency, "paid_at": paid,
		"buyer_country": input.BuyerCountry, "buyer_tax_id": input.BuyerTaxID, "details": data.details})
	if err != nil {
		return OrderInvoice{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.order_invoice_documents
		(order_id,number,buyer_name,buyer_address,seller_address,issued_at,snapshot,pdf) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		o.ID, number, input.BuyerName, input.BuyerAddress, data.sellerAddress, issued, snapshot, pdf)
	return OrderInvoice{Number: number, PDF: pdf}, err
}

func (h *handler) orderInvoice(w http.ResponseWriter, r *http.Request, a Actor) {
	var document OrderInvoice
	var err error
	if r.Method == http.MethodPost {
		var input IssueOrderInvoiceInput
		if err = readJSON(w, r, &input); err != nil {
			respond(w, nil, ErrInvalid)
			return
		}
		document, err = h.s.IssueOrderInvoice(r.Context(), a.UserID, r.PathValue("trade_no"), input)
	} else {
		if number := r.URL.Query().Get("number"); number != "" {
			document, err = h.s.DownloadOrderInvoiceNumber(r.Context(), a.UserID, r.PathValue("trade_no"), number)
		} else {
			document, err = h.s.DownloadOrderInvoice(r.Context(), a.UserID, r.PathValue("trade_no"))
		}
	}
	h.respondOrderInvoice(w, document, err)
}

func (h *handler) respondOrderInvoice(w http.ResponseWriter, document OrderInvoice, err error) {
	switch {
	case errors.Is(err, ErrInvoiceCharactersUnsupported):
		writeFailure(w, http.StatusBadRequest, "发票信息包含暂不支持的字符，请移除表情或特殊符号后重试")
	case errors.Is(err, ErrInvoiceDetailsRequired):
		writeFailure(w, http.StatusPreconditionRequired, "请先填写发票抬头和购买方地址")
	case errors.Is(err, ErrInvoiceDetailsConflict):
		writeFailure(w, http.StatusConflict, "发票已开具，请通过更正流程更新买方信息")
	case errors.Is(err, ErrInvoiceSellerMissing):
		writeFailure(w, http.StatusServiceUnavailable, "开票主体地址未配置，请联系平台")
	case err != nil:
		respond(w, nil, err)
	default:
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="`+document.Number+`.pdf"`)
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Length", fmt.Sprint(len(document.PDF)))
		_, _ = w.Write(document.PDF)
	}
}
