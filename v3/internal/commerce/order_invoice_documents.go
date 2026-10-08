package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type CorrectOrderInvoiceInput struct {
	IssueOrderInvoiceInput
	PreviousNumber string `json:"previous_number"`
	RequestID      string `json:"request_id"`
	Reason         string `json:"reason"`
}

type OrderInvoiceDocument struct {
	Number        string    `json:"number"`
	DocumentType  string    `json:"document_type"`
	Revision      int       `json:"revision"`
	Status        string    `json:"status"`
	RelatedNumber string    `json:"related_number"`
	Reason        string    `json:"reason"`
	IssuedAt      time.Time `json:"issued_at"`
	AmountMinor   int64     `json:"amount_minor"`
	Currency      string    `json:"currency"`
	BuyerName     string    `json:"buyer_name"`
	BuyerAddress  string    `json:"buyer_address"`
	BuyerCountry  string    `json:"buyer_country"`
	BuyerTaxID    string    `json:"buyer_tax_id"`
}

type OrderInvoiceDocumentPage struct {
	Items    []OrderInvoiceDocument `json:"items"`
	Total    int64                  `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"page_size"`
}

type storedOrderInvoice struct {
	OrderInvoice
	input         IssueOrderInvoiceInput
	sellerAddress string
	issued        time.Time
	snapshot      []byte
	revision      int
	previous      string
	reason        string
}

const orderInvoiceDocumentUnion = `SELECT number,'invoice'::text document_type,1 revision,''::text related_number,''::text reason,
 buyer_name,buyer_address,coalesce(snapshot->>'buyer_country','') buyer_country,coalesce(snapshot->>'buyer_tax_id','') buyer_tax_id,
 seller_address,issued_at,snapshot,pdf,(snapshot->>'amount_minor')::bigint amount_minor,NULL::bigint refund_total_minor
 FROM v3_commerce.order_invoice_documents WHERE order_id=$1
 UNION ALL SELECT number,document_type,revision,related_number,reason,buyer_name,buyer_address,buyer_country,buyer_tax_id,
 seller_address,issued_at,snapshot,pdf,amount_minor,refund_total_minor FROM v3_commerce.order_invoice_revisions WHERE order_id=$1`

func loadLatestOrderInvoice(ctx context.Context, tx pgx.Tx, orderID int64) (storedOrderInvoice, error) {
	return scanStoredOrderInvoice(tx.QueryRow(ctx, `SELECT number,pdf,buyer_name,buyer_address,buyer_country,buyer_tax_id,
 seller_address,issued_at,snapshot,revision,related_number,reason FROM (`+orderInvoiceDocumentUnion+`) d
 WHERE document_type='invoice' ORDER BY revision DESC LIMIT 1`, orderID))
}

func scanStoredOrderInvoice(row pgx.Row) (storedOrderInvoice, error) {
	var d storedOrderInvoice
	err := row.Scan(&d.Number, &d.PDF, &d.input.BuyerName, &d.input.BuyerAddress, &d.input.BuyerCountry,
		&d.input.BuyerTaxID, &d.sellerAddress, &d.issued, &d.snapshot, &d.revision, &d.previous, &d.reason)
	return d, err
}

func (s *Service) lockedInvoiceOrder(ctx context.Context, tx pgx.Tx, uid int64, trade string) (Order, error) {
	if uid <= 0 || strings.TrimSpace(trade) == "" || len(trade) > 256 {
		return Order{}, ErrInvalid
	}
	o, err := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE user_id=$1 AND trade_no=$2 FOR UPDATE`, uid, trade))
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, err
	}
	if (o.State != "paid" && o.State != "refunded") || o.PaidAt == nil || o.AmountMinor <= 0 {
		return Order{}, ErrStateConflict
	}
	return o, nil
}

func (s *Service) CorrectOrderInvoice(ctx context.Context, uid int64, trade string, in CorrectOrderInvoiceInput) (OrderInvoice, error) {
	in.IssueOrderInvoiceInput = normalizeInvoiceInput(in.IssueOrderInvoiceInput)
	in.PreviousNumber, in.RequestID, in.Reason = strings.TrimSpace(in.PreviousNumber), strings.TrimSpace(in.RequestID), strings.TrimSpace(in.Reason)
	if !validInvoiceInput(in.IssueOrderInvoiceInput) || !invoiceTextValid(in.PreviousNumber, 64, false) || !invoiceTextValid(in.RequestID, 128, false) || !invoiceTextValid(in.Reason, 300, false) {
		return OrderInvoice{}, ErrInvalid
	}
	var result OrderInvoice
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		o, err := s.lockedInvoiceOrder(ctx, tx, uid, trade)
		if err != nil {
			return err
		}
		// Request identity survives subsequent corrections; never rewrite history.
		existing, err := scanStoredOrderInvoice(tx.QueryRow(ctx, `SELECT number,pdf,buyer_name,buyer_address,buyer_country,buyer_tax_id,seller_address,issued_at,snapshot,revision,related_number,reason
 FROM v3_commerce.order_invoice_revisions WHERE order_id=$1 AND request_id=$2`, o.ID, in.RequestID))
		if err == nil {
			if existing.input != in.IssueOrderInvoiceInput || existing.previous != in.PreviousNumber || existing.reason != in.Reason {
				return ErrInvoiceDetailsConflict
			}
			result = existing.OrderInvoice
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var refund bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.provider_refund_progress WHERE order_id=$1)
 OR EXISTS(SELECT 1 FROM v3_commerce.user_refunds WHERE order_id=$1 AND status<>'failed')`, o.ID).Scan(&refund); err != nil {
			return err
		}
		if o.State != "paid" || refund {
			return ErrStateConflict
		}
		old, err := loadLatestOrderInvoice(ctx, tx, o.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvoiceDetailsRequired
		}
		if err != nil {
			return err
		}
		if old.Number != in.PreviousNumber {
			return ErrInvoiceDetailsConflict
		}
		if old.input == in.IssueOrderInvoiceInput {
			return ErrInvalid
		}
		data, err := invoiceDataFromStored(old)
		if err != nil {
			return err
		}
		data.number = fmt.Sprintf("CG-%04d-%012d-R%d", old.issued.In(time.FixedZone("HKT", 28800)).Year(), o.ID, old.revision+1)
		data.buyer, data.buyerAddress, data.buyerCountry, data.buyerTaxID = in.BuyerName, in.BuyerAddress, in.BuyerCountry, in.BuyerTaxID
		data.issued = s.cfg.Now().UTC()
		data.documentType = "invoice"
		data.relatedNumber = old.Number
		data.correctionReason = in.Reason
		result, err = s.appendOrderInvoice(ctx, tx, o.ID, old.revision+1, in.RequestID, data, 0)
		return err
	})
	return result, err
}

func invoiceDataFromStored(old storedOrderInvoice) (orderInvoiceData, error) {
	var snapshot struct {
		Description       string    `json:"description"`
		Trade             string    `json:"trade_no"`
		Provider          string    `json:"provider"`
		ProviderReference string    `json:"payment_reference"`
		SellerBRN         string    `json:"seller_brn"`
		AmountMinor       int64     `json:"amount_minor"`
		Currency          string    `json:"currency"`
		Paid              time.Time `json:"paid_at"`
		Details           []string  `json:"details"`
	}
	if err := json.Unmarshal(old.snapshot, &snapshot); err != nil {
		return orderInvoiceData{}, err
	}
	if snapshot.AmountMinor <= 0 || snapshot.Currency == "" {
		return orderInvoiceData{}, ErrStateConflict
	}
	return orderInvoiceData{number: old.Number, buyer: old.input.BuyerName, buyerAddress: old.input.BuyerAddress,
		buyerCountry: old.input.BuyerCountry, buyerTaxID: old.input.BuyerTaxID, sellerAddress: old.sellerAddress, sellerBRN: snapshot.SellerBRN,
		description: snapshot.Description, trade: snapshot.Trade, provider: snapshot.Provider, paymentReference: snapshot.ProviderReference,
		currency: strings.ToUpper(snapshot.Currency), amount: formatCurrencyMinor(snapshot.AmountMinor, strings.ToLower(snapshot.Currency)), paid: snapshot.Paid, issued: old.issued,
		details: snapshot.Details, documentType: "invoice"}, nil
}

func (s *Service) appendOrderInvoice(ctx context.Context, tx pgx.Tx, orderID int64, revision int, requestID string, data orderInvoiceData, refundTotal int64) (OrderInvoice, error) {
	// Amount remains integer until the final formatting step.
	var originalAmount int64
	if err := tx.QueryRow(ctx, `SELECT (snapshot->>'amount_minor')::bigint FROM v3_commerce.order_invoice_documents WHERE order_id=$1`, orderID).Scan(&originalAmount); err != nil {
		return OrderInvoice{}, err
	}
	amount := originalAmount
	if data.documentType == "credit_note" {
		var previous int64
		if err := tx.QueryRow(ctx, `SELECT coalesce(max(refund_total_minor),0) FROM v3_commerce.order_invoice_revisions WHERE order_id=$1 AND document_type='credit_note'`, orderID).Scan(&previous); err != nil {
			return OrderInvoice{}, err
		}
		amount = refundTotal - previous
		if amount <= 0 {
			return OrderInvoice{}, ErrStateConflict
		}
		data.amount = formatCurrencyMinor(amount, strings.ToLower(data.currency))
	}
	snapshot, err := json.Marshal(map[string]any{"seller_name": "CodeGo AI Limited", "seller_name_zh": "碼高智能有限公司", "seller_brn": data.sellerBRN, "description": data.description,
		"trade_no": data.trade, "provider": data.provider, "payment_reference": data.paymentReference, "amount_minor": amount, "currency": strings.ToLower(data.currency),
		"paid_at": data.paid, "buyer_country": data.buyerCountry, "buyer_tax_id": data.buyerTaxID, "details": data.details,
		"related_number": data.relatedNumber, "reason": data.correctionReason, "refund_total_minor": refundTotal})
	if err != nil {
		return OrderInvoice{}, err
	}
	pdf, err := renderOrderInvoice(data)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			err = errors.Join(ErrInvoiceCharactersUnsupported, err)
		}
		return OrderInvoice{}, err
	}
	var req any
	var total any
	if requestID != "" {
		req = requestID
	}
	if refundTotal > 0 {
		total = refundTotal
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.order_invoice_revisions(number,order_id,document_type,revision,related_number,request_id,reason,
 buyer_name,buyer_address,buyer_country,buyer_tax_id,seller_address,amount_minor,refund_total_minor,issued_at,snapshot,pdf)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, data.number, orderID, data.documentType, revision, data.relatedNumber, req, data.correctionReason,
		data.buyer, data.buyerAddress, data.buyerCountry, data.buyerTaxID, data.sellerAddress, amount, total, data.issued, snapshot, pdf)
	return OrderInvoice{Number: data.number, PDF: pdf}, err
}

func (s *Service) DownloadOrderCreditNote(ctx context.Context, uid int64, trade string) (OrderInvoice, error) {
	var result OrderInvoice
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		o, err := s.lockedInvoiceOrder(ctx, tx, uid, trade)
		if err != nil {
			return err
		}
		old, err := loadLatestOrderInvoice(ctx, tx, o.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvoiceDetailsRequired
		}
		if err != nil {
			return err
		}
		var total int64
		var confirmedAt time.Time
		// These sources can describe the same refund; adding them double-counts it.
		err = tx.QueryRow(ctx, `SELECT amount_minor,updated_at FROM (
 SELECT amount_minor,updated_at FROM v3_commerce.provider_refund_progress WHERE order_id=$1
 UNION ALL SELECT amount_minor,updated_at FROM v3_commerce.user_refunds WHERE order_id=$1 AND status='success'
 ) confirmed ORDER BY amount_minor DESC,updated_at DESC LIMIT 1`, o.ID).Scan(&total, &confirmedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrStateConflict
		}
		if err != nil {
			return err
		}
		if total <= 0 || total > o.AmountMinor {
			return ErrStateConflict
		}
		err = tx.QueryRow(ctx, `SELECT number,pdf FROM v3_commerce.order_invoice_revisions WHERE order_id=$1 AND refund_total_minor=$2`, o.ID, total).Scan(&result.Number, &result.PDF)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		data, err := invoiceDataFromStored(old)
		if err != nil {
			return err
		}
		var revision int
		if err = tx.QueryRow(ctx, `SELECT coalesce(max(revision),0)+1 FROM v3_commerce.order_invoice_revisions WHERE order_id=$1 AND document_type='credit_note'`, o.ID).Scan(&revision); err != nil {
			return err
		}
		data.number = fmt.Sprintf("CG-CN-%04d-%012d-%d", old.issued.In(time.FixedZone("HKT", 28800)).Year(), o.ID, revision)
		data.documentType = "credit_note"
		data.relatedNumber = old.Number
		data.correctionReason = "Confirmed payment refund"
		data.issued = s.cfg.Now().UTC()
		data.paid = confirmedAt.UTC()
		data.description = "Refund / " + data.description
		result, err = s.appendOrderInvoice(ctx, tx, o.ID, revision, "", data, total)
		return err
	})
	return result, err
}

func (s *Service) DownloadOrderInvoiceNumber(ctx context.Context, uid int64, trade, number string) (OrderInvoice, error) {
	if !invoiceTextValid(number, 64, false) {
		return OrderInvoice{}, ErrInvalid
	}
	var result OrderInvoice
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		o, err := s.lockedInvoiceOrder(ctx, tx, uid, trade)
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `SELECT number,pdf FROM (`+orderInvoiceDocumentUnion+`) d WHERE number=$2`, o.ID, number).Scan(&result.Number, &result.PDF)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	return result, err
}

func (s *Service) ListOrderInvoiceDocuments(ctx context.Context, uid int64, trade string, page, size int) (OrderInvoiceDocumentPage, error) {
	result := OrderInvoiceDocumentPage{Items: []OrderInvoiceDocument{}, Page: page, PageSize: size}
	if page < 1 || size < 1 || size > 100 || page > 1000000 {
		return result, ErrInvalid
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		o, err := s.lockedInvoiceOrder(ctx, tx, uid, trade)
		if err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM (`+orderInvoiceDocumentUnion+`) d`, o.ID).Scan(&result.Total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT number,document_type,revision,CASE WHEN document_type='credit_note' THEN 'issued'
 WHEN revision=max(revision) FILTER(WHERE document_type='invoice') OVER() THEN 'current' ELSE 'superseded' END,
 related_number,reason,issued_at,amount_minor,coalesce(snapshot->>'currency',''),buyer_name,buyer_address,buyer_country,buyer_tax_id
 FROM (`+orderInvoiceDocumentUnion+`) d ORDER BY issued_at DESC,number DESC LIMIT $2 OFFSET $3`, o.ID, size, (page-1)*size)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d OrderInvoiceDocument
			if err = rows.Scan(&d.Number, &d.DocumentType, &d.Revision, &d.Status, &d.RelatedNumber, &d.Reason, &d.IssuedAt, &d.AmountMinor, &d.Currency, &d.BuyerName, &d.BuyerAddress, &d.BuyerCountry, &d.BuyerTaxID); err != nil {
				return err
			}
			result.Items = append(result.Items, d)
		}
		return rows.Err()
	})
	return result, err
}

func orderInvoiceDetails(o Order) []string {
	items := []string{}
	if o.Kind != "blind_box" && o.Credits > 0 {
		items = append(items, "Account usage credit: "+o.Credits.String()+" USD equivalent (not the payment amount)")
	}
	if o.Kind == "subscription" && o.PeriodSeconds > 0 {
		items = append(items, "Subscription duration: "+strconv.FormatInt(o.PeriodSeconds, 10)+" seconds from activation")
	}
	if o.Kind == "fuel" && o.FuelExpiresAt != nil {
		items = append(items, "Additional credit expires: "+o.FuelExpiresAt.UTC().Format(time.RFC3339))
	}
	return items
}

func (h *handler) orderInvoiceCorrection(w http.ResponseWriter, r *http.Request, a Actor) {
	var input CorrectOrderInvoiceInput
	if err := readJSON(w, r, &input); err != nil {
		respond(w, nil, ErrInvalid)
		return
	}
	d, err := h.s.CorrectOrderInvoice(r.Context(), a.UserID, r.PathValue("trade_no"), input)
	h.respondOrderInvoice(w, d, err)
}

func (h *handler) orderCreditNote(w http.ResponseWriter, r *http.Request, a Actor) {
	var input map[string]json.RawMessage
	err := readJSON(w, r, &input)
	if (err != nil && !errors.Is(err, io.EOF)) || (err == nil && (input == nil || len(input) != 0)) {
		respond(w, nil, ErrInvalid)
		return
	}
	d, err := h.s.DownloadOrderCreditNote(r.Context(), a.UserID, r.PathValue("trade_no"))
	h.respondOrderInvoice(w, d, err)
}

func (h *handler) orderInvoiceDocuments(w http.ResponseWriter, r *http.Request, a Actor) {
	w.Header().Set("Cache-Control", "private, no-store")
	page, size := 1, 50
	for key, ptr := range map[string]*int{"page": &page, "page_size": &size} {
		if raw := r.URL.Query().Get(key); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				respond(w, nil, ErrInvalid)
				return
			}
			*ptr = n
		}
	}
	d, err := h.s.ListOrderInvoiceDocuments(r.Context(), a.UserID, r.PathValue("trade_no"), page, size)
	respond(w, d, err)
}
