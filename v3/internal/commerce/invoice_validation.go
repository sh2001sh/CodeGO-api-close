package commerce

import (
	"net/mail"
	"sort"
	"strings"
	"unicode/utf8"
)

func validateInvoiceCreate(input *CreateInvoiceRequestInput) error {
	input.SourceType, input.TradeNo = strings.TrimSpace(input.SourceType), strings.TrimSpace(input.TradeNo)
	if len(input.Orders) == 0 && input.SourceType != "" && input.TradeNo != "" {
		input.Orders = []InvoiceOrderInput{{SourceType: input.SourceType, TradeNo: input.TradeNo}}
	}
	if len(input.Orders) == 0 || len(input.Orders) > 100 {
		return ErrInvalid
	}
	seen := make(map[string]bool, len(input.Orders))
	for i := range input.Orders {
		o := &input.Orders[i]
		o.SourceType, o.TradeNo = strings.TrimSpace(o.SourceType), strings.TrimSpace(o.TradeNo)
		if (o.SourceType != "topup" && o.SourceType != "subscription") || o.TradeNo == "" || len(o.TradeNo) > 255 || seen[o.TradeNo] {
			return ErrInvalid
		}
		seen[o.TradeNo] = true
	}
	// A consistent lock order avoids deadlocks between overlapping batches.
	sort.Slice(input.Orders, func(i, j int) bool { return input.Orders[i].TradeNo < input.Orders[j].TradeNo })
	input.InvoiceType, input.Title = strings.TrimSpace(input.InvoiceType), strings.TrimSpace(input.Title)
	input.TaxNumber, input.Email, input.Remark = strings.TrimSpace(input.TaxNumber), strings.TrimSpace(input.Email), strings.TrimSpace(input.Remark)
	if (input.InvoiceType != "personal" && input.InvoiceType != "enterprise") || input.Title == "" || utf8.RuneCountInString(input.Title) > 255 ||
		utf8.RuneCountInString(input.TaxNumber) > 64 || utf8.RuneCountInString(input.Remark) > 500 || len(input.Email) > 255 {
		return ErrInvalid
	}
	if input.InvoiceType == "enterprise" && utf8.RuneCountInString(input.TaxNumber) < 8 {
		return ErrInvalid
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email {
		return ErrInvalid
	}
	return nil
}

func validateInvoiceUpdate(input *UpdateInvoiceRequestInput) error {
	input.Status, input.InvoiceNumber, input.AdminNote = strings.TrimSpace(input.Status), strings.TrimSpace(input.InvoiceNumber), strings.TrimSpace(input.AdminNote)
	if (input.Status != "issued" && input.Status != "rejected") || utf8.RuneCountInString(input.InvoiceNumber) > 128 || utf8.RuneCountInString(input.AdminNote) > 1000 {
		return ErrInvalid
	}
	if input.Status == "issued" && input.InvoiceNumber == "" || input.Status == "rejected" && (input.AdminNote == "" || input.InvoiceNumber != "") {
		return ErrInvalid
	}
	return nil
}
