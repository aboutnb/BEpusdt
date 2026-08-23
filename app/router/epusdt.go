package router

import (
	"github.com/gin-gonic/gin"
	"github.com/v03413/bepusdt/app/handler/epusdt"
)

func epusdtInit(engine *gin.Engine) {
	epGrp := engine.Group("/pay")
	epHdr := new(epusdt.Epusdt)
	{
		epGrp.GET("/checkout/:trade_id", epHdr.Checkout)
	}

	orderGrp := engine.Group("/api/v1/order")
	{
		orderGrp.Use(epHdr.SignVerify)
		orderGrp.POST("/create-transaction", epHdr.CreateTransaction)
		orderGrp.POST("/cancel-transaction", epHdr.CancelTransaction)
		orderGrp.POST("/create-order", epHdr.CreateOrder)
	}

	payGrp := engine.Group("/api/v1/pay")
	{
		payGrp.POST("/info", epHdr.Info)
		payGrp.POST("/notify", epHdr.Notify)
		payGrp.POST("/methods", epHdr.GetMethods)
		payGrp.POST("/update-order", epHdr.UpdateOrder)
	}

	merchantGrp := engine.Group("/api/v1/merchant")
	merchantGrp.Use(epHdr.MerchantSignVerify)
	{
		merchantGrp.POST("/order/create", epHdr.MerchantCreate)
		merchantGrp.POST("/order/query", epHdr.MerchantQuery)
		merchantGrp.POST("/capabilities", epHdr.MerchantCapabilities)
		merchantGrp.POST("/rate", epHdr.MerchantRate)
		merchantGrp.GET("/readiness", epHdr.MerchantReadiness)
	}
}
