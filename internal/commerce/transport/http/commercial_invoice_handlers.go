package http

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
)

func commercialInvoiceHandler(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		var input *commerceapp.CommercialInvoiceInput
		if action == "issue" || action == "correct" {
			input = &commerceapp.CommercialInvoiceInput{}
			if c.ShouldBindJSON(input) != nil {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invoice.invalidDetails"})
				return
			}
		}
		documents, err := commerceapp.CommercialInvoice(c.GetInt("id"), c.Param("source"), c.Param("trade_no"), action, c.Query("number"), input)
		if err != nil {
			status, message := http.StatusInternalServerError, "invoice.downloadFailed"
			switch {
			case errors.Is(err, commerceapp.ErrCommercialInvoiceDetailsRequired):
				status, message = http.StatusPreconditionRequired, "invoice.detailsRequired"
			case errors.Is(err, commerceapp.ErrCommercialInvoiceInvalid):
				status, message = http.StatusBadRequest, "invoice.invalidDetails"
			case errors.Is(err, commerceapp.ErrCommercialInvoiceConflict):
				status, message = http.StatusConflict, "invoice.detailsConflict"
			case errors.Is(err, commerceapp.ErrInvoiceOrderUnavailable):
				status, message = http.StatusNotFound, "invoice.orderUnavailable"
			}
			c.JSON(status, gin.H{"success": false, "message": message})
			return
		}
		if action == "list" {
			for index := range documents {
				documents[index].PDF = nil
			}
			c.JSON(http.StatusOK, gin.H{"success": true, "data": documents})
			return
		}
		if len(documents) != 1 {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "invoice.downloadFailed"})
			return
		}
		c.Header("Content-Disposition", `attachment; filename="`+documents[0].Number+`.pdf"`)
		c.Data(http.StatusOK, "application/pdf", documents[0].PDF)
	}
}
