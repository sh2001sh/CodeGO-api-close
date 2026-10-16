package http

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
)

func ListShops(c *gin.Context) {
	result, err := marketplaceapp.ListMarketplaceShops(marketplaceapp.GroupQuery{
		ViewerUserID: c.GetInt("id"), Search: c.Query("search"), Tag: c.Query("tag"),
		Model: c.Query("model"), Models: c.QueryArray("model"), Source: c.Query("source"),
		MultiplierCard: c.Query("multiplier_card"), Provider: c.Query("provider"),
		CreditPack: c.Query("credit_pack"),
		Sort:       c.Query("sort"), Direction: c.Query("direction"),
		Page: queryInt(c, "page", 1), PageSize: queryInt(c, "page_size", 20),
	})
	respond(c, result, err)
}

func GetMyShop(c *gin.Context) {
	result, err := marketplaceapp.GetOwnerShop(c.GetInt("id"))
	respond(c, result, err)
}

func UpdateMyShop(c *gin.Context) {
	var req marketplaceapp.ShopProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, nil, err)
		return
	}
	result, err := marketplaceapp.UpdateOwnerShop(c.GetInt("id"), req)
	respond(c, result, err)
}

func RateShop(c *gin.Context) {
	ownerID, err := strconv.Atoi(c.Param("id"))
	if err != nil || ownerID <= 0 {
		respond(c, nil, errors.New("店铺不存在"))
		return
	}
	var req struct {
		Stars int `json:"stars"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, nil, err)
		return
	}
	result, err := marketplaceapp.RateMarketplaceShop(c.GetInt("id"), ownerID, req.Stars)
	respond(c, result, err)
}

func SubmitReport(c *gin.Context) {
	var req marketplaceapp.MarketReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, nil, err)
		return
	}
	err := marketplaceapp.SubmitMarketReport(c.GetInt("id"), req)
	respond(c, gin.H{"submitted": err == nil}, err)
}

func ListReports(c *gin.Context) {
	result, err := marketplaceapp.ListMarketReports(c.Query("status"), queryInt(c, "page", 1), queryInt(c, "page_size", 20))
	respond(c, result, err)
}

func ResolveReport(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		respond(c, nil, errors.New("举报不存在"))
		return
	}
	var req struct {
		Action string `json:"action"`
		Note   string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, nil, err)
		return
	}
	err = marketplaceapp.ResolveMarketReport(c.GetInt("id"), id, req.Action, req.Note)
	respond(c, gin.H{"resolved": err == nil}, err)
}
