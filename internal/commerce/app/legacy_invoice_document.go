package app

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

const legacyInvoiceArchivePrefix = "codego-legacy-invoice-gzip:"

var ErrLegacyInvoiceDocumentUnavailable = errors.New("invoice.documentUnavailable")

type legacyInvoiceArchive struct {
	Version   int                         `json:"version"`
	Documents []CommercialInvoiceDocument `json:"documents"`
}

// EncodeLegacyInvoiceDocuments freezes original legacy PDFs in the existing
// private document field. It is used by the one-time audited import and tests.
func EncodeLegacyInvoiceDocuments(documents []CommercialInvoiceDocument) (string, error) {
	if len(documents) == 0 {
		return "", ErrLegacyInvoiceDocumentUnavailable
	}
	archive := legacyInvoiceArchive{Version: 1, Documents: documents}
	for index := range archive.Documents {
		document := &archive.Documents[index]
		if !invoiceTextValid(document.Number, 128, false) || len(document.PDF) == 0 || len(document.PDF) > 16<<20 || !bytes.HasPrefix(document.PDF, []byte("%PDF-")) {
			return "", ErrLegacyInvoiceDocumentUnavailable
		}
	}
	raw, err := json.Marshal(archive)
	if err != nil {
		return "", err
	}
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err = writer.Write(raw); err != nil {
		return "", err
	}
	if err = writer.Close(); err != nil {
		return "", err
	}
	return legacyInvoiceArchivePrefix + base64.StdEncoding.EncodeToString(buffer.Bytes()), nil
}

func decodeLegacyInvoiceDocuments(value string) ([]CommercialInvoiceDocument, error) {
	if !strings.HasPrefix(value, legacyInvoiceArchivePrefix) {
		return nil, ErrLegacyInvoiceDocumentUnavailable
	}
	compressed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, legacyInvoiceArchivePrefix))
	if err != nil {
		return nil, ErrLegacyInvoiceDocumentUnavailable
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, ErrLegacyInvoiceDocumentUnavailable
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, 32<<20))
	if err != nil {
		return nil, ErrLegacyInvoiceDocumentUnavailable
	}
	archive := legacyInvoiceArchive{}
	if json.Unmarshal(raw, &archive) != nil || archive.Version != 1 || len(archive.Documents) == 0 {
		return nil, ErrLegacyInvoiceDocumentUnavailable
	}
	for _, document := range archive.Documents {
		if !invoiceTextValid(document.Number, 128, false) || len(document.PDF) == 0 || len(document.PDF) > 16<<20 || !bytes.HasPrefix(document.PDF, []byte("%PDF-")) {
			return nil, ErrLegacyInvoiceDocumentUnavailable
		}
	}
	return archive.Documents, nil
}

func LegacyInvoiceDocument(userID int, requestID int64, number string) (*CommercialInvoiceDocument, error) {
	if userID <= 0 || requestID <= 0 {
		return nil, ErrLegacyInvoiceDocumentUnavailable
	}
	var request commerceschema.InvoiceRequest
	err := platformdb.DB.Select("invoice_number", "document_url").
		Where("id = ? AND user_id = ? AND status = ?", requestID, userID, commerceschema.InvoiceStatusIssued).
		First(&request).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLegacyInvoiceDocumentUnavailable
		}
		return nil, err
	}
	documents, err := decodeLegacyInvoiceDocuments(request.DocumentURL)
	if err != nil {
		return nil, err
	}
	if number = strings.TrimSpace(number); number == "" {
		number = request.InvoiceNumber
	}
	for index := range documents {
		if documents[index].Number == number {
			return &documents[index], nil
		}
	}
	return nil, ErrLegacyInvoiceDocumentUnavailable
}

func markLegacyInvoiceDocumentsAvailable(requests []commerceschema.InvoiceRequest) error {
	if len(requests) == 0 {
		return nil
	}
	ids := make([]int64, len(requests))
	for index := range requests {
		ids[index] = requests[index].ID
	}
	var available []int64
	if err := platformdb.DB.Model(&commerceschema.InvoiceRequest{}).Where("id IN ? AND document_url LIKE ?", ids, legacyInvoiceArchivePrefix+"%").Pluck("id", &available).Error; err != nil {
		return err
	}
	set := make(map[int64]struct{}, len(available))
	for _, id := range available {
		set[id] = struct{}{}
	}
	for index := range requests {
		_, requests[index].DocumentAvailable = set[requests[index].ID]
	}
	return nil
}
