package router

import (
	"github.com/gin-gonic/gin"
	"github.com/v03413/bepusdt/app/handler/merchant"
)

func merchantInit(engine *gin.Engine) {
	handler := new(merchant.API)
	engine.POST("/api/v1/merchant/rate", new(merchant.Rate).Quote)
	engine.POST("/api/v1/merchant/capabilities", handler.Capabilities)
	engine.GET("/api/v1/merchant/readiness", handler.Readiness)
	engine.POST("/api/v1/merchant/order/create", handler.CreateOrder)
	engine.POST("/api/v1/merchant/order/query", handler.QueryOrder)
}
