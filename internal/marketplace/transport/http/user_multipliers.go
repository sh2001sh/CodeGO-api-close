package http

import (
	"errors"
	"github.com/gin-gonic/gin"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	"strconv"
)

func ListOwnerMultipliers(c *gin.Context) {
	page := queryInt(c, "page", 1)
	items, total, err := marketplaceapp.ListOwnerMultipliers(c.GetInt("id"), page)
	respond(c, gin.H{"items": items, "total": total}, err)
}

func BatchSetUserMultipliers(c *gin.Context) {
	var req struct {
		Targets    []marketplaceapp.MultiplierTarget `json:"targets"`
		Action     string                            `json:"action"`
		Multiplier *float64                          `json:"multiplier"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, nil, err)
		return
	}
	if (req.Action != "set" && req.Action != "clear") || (req.Action == "set" && req.Multiplier == nil) || (req.Action == "clear" && req.Multiplier != nil) {
		respond(c, nil, errors.New("请选择设置倍率或清除专属倍率"))
		return
	}
	count, err := marketplaceapp.BatchSetUserMultipliers(c.GetInt("id"), req.Targets, req.Multiplier)
	respond(c, gin.H{"changed_count": count}, err)
}

func ListMultiplierNotices(c *gin.Context) {
	items, err := marketplaceapp.ListMultiplierNotices(c.GetInt("id"))
	respond(c, items, err)
}

func ReadMultiplierNotice(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		respond(c, nil, errors.New("通知编号无效"))
		return
	}
	respond(c, gin.H{"read": true}, marketplaceapp.ReadMultiplierNotice(c.GetInt("id"), id))
}
