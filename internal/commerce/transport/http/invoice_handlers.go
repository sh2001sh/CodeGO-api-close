package http

import (
	"strconv"

	"github.com/gin-gonic/gin"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

func listInvoiceEligibleOrders(c *gin.Context) {
	orders, err := commerceapp.ListInvoiceEligibleOrders(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, orders)
}

func listSelfInvoiceRequests(c *gin.Context) {
	payload, err := commerceapp.ListUserInvoiceRequests(c.GetInt("id"), platformpagination.GetPageQuery(c))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func createInvoiceRequest(c *gin.Context) {
	var input commerceapp.CreateInvoiceRequestInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	request, err := commerceapp.CreateInvoiceRequest(c.GetInt("id"), input)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, request)
}

func listAdminInvoiceRequests(c *gin.Context) {
	payload, err := commerceapp.ListAdminInvoiceRequests(c.Query("status"), platformpagination.GetPageQuery(c))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func updateAdminInvoiceRequest(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		httpapi.ApiErrorMsg(c, "invalid id")
		return
	}
	var input commerceapp.UpdateInvoiceRequestInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	request, err := commerceapp.UpdateAdminInvoiceRequest(id, c.GetInt("id"), input)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, request)
}
