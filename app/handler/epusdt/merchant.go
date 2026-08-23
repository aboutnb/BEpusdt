package epusdt

import (
	"bytes"
	"crypto/hmac"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/spf13/cast"
	appconf "github.com/v03413/bepusdt/app/conf"
	"github.com/v03413/bepusdt/app/model"
	"github.com/v03413/bepusdt/app/utils"
)

const maxMerchantBodySize = 1 << 20

type merchantCreateReq struct {
	OrderID        string     `json:"order_id" binding:"required"`
	Amount         string     `json:"amount" binding:"required"`
	Fiat           model.Fiat `json:"fiat" binding:"required"`
	TradeType      string     `json:"trade_type" binding:"required"`
	Rate           string     `json:"rate"`
	NotifyURL      string     `json:"notify_url" binding:"required"`
	RedirectURL    string     `json:"redirect_url" binding:"required"`
	Name           string     `json:"name"`
	TimeoutSeconds int64      `json:"timeout_seconds"`
}

type merchantQueryReq struct {
	OrderID string `json:"order_id"`
	TradeID string `json:"trade_id"`
}

type merchantCapabilitiesReq struct {
	Networks []string `json:"networks"`
}

type merchantRateReq struct {
	Crypto string `json:"crypto" binding:"required"`
	Fiat   string `json:"fiat" binding:"required"`
}

type merchantCapability struct {
	Crypto           string `json:"crypto"`
	Network          string `json:"network"`
	NetworkName      string `json:"network_name"`
	TradeType        string `json:"trade_type"`
	WalletCount      int    `json:"wallet_count"`
	RPCEndpointSet   bool   `json:"rpc_endpoint_set"`
	RPCEndpointCount int    `json:"rpc_endpoint_count"`
	ScannerBlock     string `json:"scanner_block"`
	ScannerSuccess   string `json:"scanner_success"`
	LastScanAt       int64  `json:"last_scan_at"`
	ChainHead        int64  `json:"chain_head"`
	ScannerLag       int64  `json:"scanner_lag"`
	QueueDepth       int    `json:"queue_depth"`
	AcceptingOrders  bool   `json:"accepting_orders"`
	Reason           string `json:"reason,omitempty"`
}

type merchantOrderView struct {
	OrderID            string         `json:"order_id"`
	TradeID            string         `json:"trade_id"`
	Status             int            `json:"status"`
	StatusName         string         `json:"status_name"`
	Fiat               string         `json:"fiat"`
	Amount             string         `json:"amount"`
	Crypto             string         `json:"crypto"`
	ActualAmount       string         `json:"actual_amount"`
	ExchangeRate       string         `json:"exchange_rate"`
	TradeType          string         `json:"trade_type"`
	Network            string         `json:"network"`
	Token              string         `json:"token"`
	BlockTransactionID string         `json:"block_transaction_id"`
	TransferAt         int64          `json:"transfer_at"`
	CreatedAt          int64          `json:"created_at"`
	ExpiresAt          int64          `json:"expires_at"`
	PaymentURL         string         `json:"payment_url,omitempty"`
	Confirmation       map[string]any `json:"confirmation"`
}

func (Epusdt) MerchantSignVerify(ctx *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, maxMerchantBodySize+1))
	if err != nil || len(body) > maxMerchantBodySize {
		merchantError(ctx, http.StatusBadRequest, "invalid request body")
		ctx.Abort()
		return
	}
	ctx.Request.Body = io.NopCloser(bytes.NewReader(body))

	keyID := ctx.GetHeader(utils.HMACKeyIDHeader)
	timestamp := ctx.GetHeader(utils.HMACTimestampHeader)
	nonce := ctx.GetHeader(utils.HMACNonceHeader)
	digest := strings.ToLower(ctx.GetHeader(utils.HMACDigestHeader))
	signature := strings.ToLower(ctx.GetHeader(utils.HMACSignatureHeader))
	if keyID == "" || timestamp == "" || nonce == "" || digest == "" || signature == "" {
		merchantError(ctx, http.StatusUnauthorized, "missing HMAC v2 headers")
		ctx.Abort()
		return
	}
	if keyID != model.MerchantKeyID() {
		merchantError(ctx, http.StatusUnauthorized, "unknown key id")
		ctx.Abort()
		return
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	skew := int64(cast.ToInt(model.GetC(model.ApiHMACClockSkew)))
	if skew <= 0 {
		skew = 300
	}
	now := time.Now()
	if err != nil || ts < now.Unix()-skew || ts > now.Unix()+skew {
		merchantError(ctx, http.StatusUnauthorized, "request timestamp outside allowed window")
		ctx.Abort()
		return
	}
	if len(nonce) < 16 || len(nonce) > 128 {
		merchantError(ctx, http.StatusUnauthorized, "invalid nonce")
		ctx.Abort()
		return
	}
	expectedDigest := utils.SHA256Hex(body)
	if !hmac.Equal([]byte(digest), []byte(expectedDigest)) {
		merchantError(ctx, http.StatusUnauthorized, "body digest mismatch")
		ctx.Abort()
		return
	}
	if !utils.HMACV2Verify(signature, model.MerchantSecret(), ctx.Request.Method, ctx.Request.URL.EscapedPath(), timestamp, nonce, digest) {
		merchantError(ctx, http.StatusUnauthorized, "invalid signature")
		ctx.Abort()
		return
	}
	if err := model.RegisterMerchantNonce(keyID, nonce, now.Add(time.Duration(skew)*time.Second)); err != nil {
		merchantError(ctx, http.StatusConflict, err.Error())
		ctx.Abort()
		return
	}
	ctx.Set("merchant_key_id", keyID)
	ctx.Next()
}

func (Epusdt) MerchantCreate(ctx *gin.Context) {
	var req merchantCreateReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		merchantError(ctx, http.StatusBadRequest, fmt.Sprintf("invalid create request: %v", err))
		return
	}
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		merchantError(ctx, http.StatusBadRequest, "amount must be a positive decimal string")
		return
	}
	tradeType := model.TradeType(req.TradeType)
	tradeConf, ok := model.GetTradeConfig(tradeType)
	if !ok || tradeConf.Crypto != model.USDT {
		merchantError(ctx, http.StatusBadRequest, "trade_type must be a supported USDT network")
		return
	}
	capability := buildMerchantCapability(tradeType, tradeConf)
	if !capability.AcceptingOrders {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": "network_not_ready", "message": capability.Reason, "network": capability})
		return
	}
	if !utils.IsAllowedCallbackURLForHosts(req.NotifyURL, model.GetC(model.ApiNotifyHosts)) {
		merchantError(ctx, http.StatusBadRequest, "notify_url host is not allowlisted")
		return
	}
	if !utils.IsAllowedCallbackURLForHosts(req.RedirectURL, model.GetC(model.ApiRedirectHosts)) {
		merchantError(ctx, http.StatusBadRequest, "redirect_url host is not allowlisted")
		return
	}
	if rate := strings.TrimSpace(req.Rate); rate != "" {
		parsed, err := decimal.NewFromString(rate)
		if err != nil || parsed.LessThanOrEqual(decimal.Zero) {
			merchantError(ctx, http.StatusBadRequest, "rate must be a positive decimal string")
			return
		}
		req.Rate = parsed.String()
	}
	order, err := model.StartBuildMerchantOrder(model.OrderParams{
		Money:       amount,
		ApiType:     model.OrderApiTypeMerchant,
		OrderId:     req.OrderID,
		TradeType:   tradeType,
		RedirectUrl: req.RedirectURL,
		NotifyUrl:   req.NotifyURL,
		Name:        req.Name,
		Timeout:     req.TimeoutSeconds,
		Fiat:        req.Fiat,
		Rate:        req.Rate,
	})
	if err != nil {
		merchantError(ctx, http.StatusConflict, err.Error())
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": "ok", "data": buildMerchantOrderView(ctx, order)})
}

func (Epusdt) MerchantRate(ctx *gin.Context) {
	var req merchantRateReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		merchantError(ctx, http.StatusBadRequest, fmt.Sprintf("invalid rate request: %v", err))
		return
	}
	crypto := model.Crypto(strings.ToUpper(strings.TrimSpace(req.Crypto)))
	fiat := model.Fiat(strings.ToUpper(strings.TrimSpace(req.Fiat)))
	if crypto != model.USDT || fiat != model.CNY {
		merchantError(ctx, http.StatusBadRequest, "only USDT/CNY rate is supported")
		return
	}
	syntax := model.GetK(model.ConfKey(fmt.Sprintf("rate_float_%s_%s", crypto, fiat)))
	rate, err := model.GetOrderRate(crypto, fiat, syntax)
	if err != nil || rate.LessThanOrEqual(decimal.Zero) {
		merchantError(ctx, http.StatusServiceUnavailable, "USDT/CNY rate is unavailable")
		return
	}
	var latest model.Rate
	model.Db.Where("crypto = ? AND fiat = ?", crypto, fiat).Order("created_at desc").Limit(1).Find(&latest)
	updatedAt := int64(0)
	if latest.CreatedAt != nil {
		updatedAt = latest.CreatedAt.Time().Unix()
	}
	ctx.JSON(http.StatusOK, gin.H{"code": "ok", "data": gin.H{
		"crypto": string(crypto), "fiat": string(fiat), "rate": rate.String(), "updated_at": updatedAt,
	}})
}

func (Epusdt) MerchantQuery(ctx *gin.Context) {
	var req merchantQueryReq
	if err := ctx.ShouldBindJSON(&req); err != nil || (req.OrderID == "" && req.TradeID == "") {
		merchantError(ctx, http.StatusBadRequest, "order_id or trade_id is required")
		return
	}
	var order model.Order
	var ok bool
	if req.TradeID != "" {
		order, ok = model.GetTradeOrder(req.TradeID)
	} else {
		order, ok = model.GetOrderByMerchantID(req.OrderID)
	}
	if !ok || order.ApiType != model.OrderApiTypeMerchant {
		merchantError(ctx, http.StatusNotFound, "order not found")
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": "ok", "data": buildMerchantOrderView(ctx, order)})
}

func (Epusdt) MerchantCapabilities(ctx *gin.Context) {
	var req merchantCapabilitiesReq
	if err := ctx.ShouldBindJSON(&req); err != nil && err != io.EOF {
		merchantError(ctx, http.StatusBadRequest, "invalid capabilities request")
		return
	}
	allowed := make(map[string]bool, len(req.Networks))
	for _, network := range req.Networks {
		allowed[strings.ToLower(strings.TrimSpace(network))] = true
	}
	items := make([]merchantCapability, 0)
	for tradeType, tradeConf := range model.GetAllTradeConfig() {
		if tradeConf.Crypto != model.USDT || len(model.GetAvailableWallets(model.TradeType(tradeType))) == 0 {
			continue
		}
		if len(allowed) > 0 && !allowed[strings.ToLower(string(tradeConf.Network))] {
			continue
		}
		items = append(items, buildMerchantCapability(model.TradeType(tradeType), tradeConf))
	}
	ctx.JSON(http.StatusOK, gin.H{"code": "ok", "data": gin.H{"networks": items}})
}

func (Epusdt) MerchantReadiness(ctx *gin.Context) {
	var callbackBacklog int64
	dbReady := model.Db != nil
	if dbReady {
		sqlDB, err := model.Db.DB()
		dbReady = err == nil && sqlDB.PingContext(ctx.Request.Context()) == nil
		_ = model.Db.Model(&model.Order{}).
			Where("api_type = ? AND status = ? AND notify_state = ?", model.OrderApiTypeMerchant, model.OrderStatusSuccess, model.OrderNotifyStateFail).
			Count(&callbackBacklog).Error
	}
	capabilities := make([]merchantCapability, 0)
	readyNetworks := 0
	for tradeType, tradeConf := range model.GetAllTradeConfig() {
		if tradeConf.Crypto != model.USDT || len(model.GetAvailableWallets(model.TradeType(tradeType))) == 0 {
			continue
		}
		capability := buildMerchantCapability(model.TradeType(tradeType), tradeConf)
		capabilities = append(capabilities, capability)
		if capability.AcceptingOrders {
			readyNetworks++
		}
	}
	backlogMax := int64(cast.ToInt(model.GetC(model.CallbackBacklogMax)))
	if backlogMax <= 0 {
		backlogMax = 50
	}
	ready := dbReady && readyNetworks > 0 && callbackBacklog < backlogMax
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	ctx.JSON(status, gin.H{"code": "ok", "data": gin.H{
		"ready": ready, "database": dbReady, "ready_networks": readyNetworks,
		"callback_backlog": callbackBacklog, "callback_backlog_max": backlogMax,
		"callback_backlog_healthy": callbackBacklog < backlogMax,
		"networks":                 capabilities, "checked_at": time.Now().Unix(),
	}})
}

func buildMerchantCapability(tradeType model.TradeType, tradeConf model.TradeTypeConf) merchantCapability {
	walletCount := len(model.GetAvailableWallets(tradeType))
	endpointSet := strings.TrimSpace(model.Endpoint(tradeConf.Network)) != ""
	endpointCount := len(strings.FieldsFunc(model.Endpoint(tradeConf.Network), func(r rune) bool { return r == ',' || r == '\n' || r == '\r' }))
	block, success, lastScan, hasScan := appconf.GetNetworkStat(string(tradeConf.Network))
	maxAge := int64(cast.ToInt(model.GetC(model.ScannerMaxAgeSeconds)))
	if maxAge <= 0 {
		maxAge = 120
	}
	minSuccess := cast.ToFloat64(model.GetC(model.ScannerMinSuccess))
	if minSuccess <= 0 {
		minSuccess = 99
	}
	successRate := cast.ToFloat64(strings.TrimSuffix(success, "%"))
	chainHead, queueDepth, _, hasRuntime := appconf.GetNetworkRuntime(string(tradeConf.Network))
	scannedBlock := cast.ToInt64(block)
	scannerLag := chainHead - scannedBlock
	if scannerLag < 0 {
		scannerLag = 0
	}
	maxLag := int64(cast.ToInt(model.GetC(model.ScannerMaxLagBlocks)))
	if maxLag <= 0 {
		maxLag = 30
	}
	queueMax := cast.ToInt(model.GetC(model.ScannerQueueMax))
	if queueMax <= 0 {
		queueMax = 80
	}
	callbackBacklog, callbackBacklogMax := merchantCallbackBacklog()
	ready := walletCount > 0 && endpointSet && hasScan && time.Now().Unix()-lastScan <= maxAge &&
		successRate >= minSuccess && (!hasRuntime || scannerLag <= maxLag) && queueDepth < queueMax &&
		callbackBacklog < callbackBacklogMax
	reason := ""
	switch {
	case walletCount == 0:
		reason = "no enabled receiving wallet"
	case !endpointSet:
		reason = "RPC endpoint is not configured"
	case !hasScan:
		reason = "scanner has not reported a successful scan"
	case time.Now().Unix()-lastScan > maxAge:
		reason = "scanner heartbeat is stale"
	case successRate < minSuccess:
		reason = fmt.Sprintf("scanner success rate %.2f%% is below %.2f%%", successRate, minSuccess)
	case hasRuntime && scannerLag > maxLag:
		reason = fmt.Sprintf("scanner lag %d blocks exceeds %d", scannerLag, maxLag)
	case queueDepth >= queueMax:
		reason = fmt.Sprintf("scanner queue depth %d exceeds %d", queueDepth, queueMax)
	case callbackBacklog >= callbackBacklogMax:
		reason = fmt.Sprintf("callback backlog %d exceeds %d", callbackBacklog, callbackBacklogMax)
	}
	return merchantCapability{
		Crypto: string(tradeConf.Crypto), Network: string(tradeConf.Network), NetworkName: tradeConf.NetworkName,
		TradeType: string(tradeType), WalletCount: walletCount, RPCEndpointSet: endpointSet, RPCEndpointCount: endpointCount,
		ScannerBlock: block, ScannerSuccess: success, LastScanAt: lastScan,
		ChainHead: chainHead, ScannerLag: scannerLag, QueueDepth: queueDepth,
		AcceptingOrders: ready, Reason: reason,
	}
}

func merchantCallbackBacklog() (int64, int64) {
	var count int64
	if model.Db != nil {
		_ = model.Db.Model(&model.Order{}).
			Where("api_type = ? AND status = ? AND notify_state = ?", model.OrderApiTypeMerchant, model.OrderStatusSuccess, model.OrderNotifyStateFail).
			Count(&count).Error
	}
	maximum := int64(cast.ToInt(model.GetC(model.CallbackBacklogMax)))
	if maximum <= 0 {
		maximum = 50
	}
	return count, maximum
}

func buildMerchantOrderView(ctx *gin.Context, order model.Order) merchantOrderView {
	tradeConf, _ := model.GetTradeConfig(order.TradeType)
	createdAt := int64(0)
	if order.CreatedAt != nil {
		createdAt = order.CreatedAt.Time().Unix()
	}
	transferAt := int64(0)
	if order.ConfirmedAt != nil && !order.ConfirmedAt.IsZero() {
		transferAt = order.ConfirmedAt.Unix()
	}
	return merchantOrderView{
		OrderID: order.OrderId, TradeID: order.TradeId, Status: order.Status, StatusName: merchantStatusName(order.Status),
		Fiat: string(order.Fiat), Amount: order.Money, Crypto: string(order.Crypto), ActualAmount: order.Amount,
		ExchangeRate: order.Rate, TradeType: string(order.TradeType), Network: string(tradeConf.Network), Token: order.Address,
		BlockTransactionID: order.RefHash, TransferAt: transferAt, CreatedAt: createdAt, ExpiresAt: order.ExpiredAt.Unix(),
		PaymentURL:   model.CheckoutUrl(utils.GetRequestHost(ctx.Request), order.TradeId),
		Confirmation: map[string]any{"block_number": order.RefBlockNum, "confirmed": order.Status == model.OrderStatusSuccess},
	}
}

func merchantStatusName(status int) string {
	switch status {
	case model.OrderStatusWaiting:
		return "waiting"
	case model.OrderStatusSuccess:
		return "succeeded"
	case model.OrderStatusExpired:
		return "expired"
	case model.OrderStatusCanceled:
		return "canceled"
	case model.OrderStatusConfirming:
		return "confirming"
	case model.OrderStatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

func merchantError(ctx *gin.Context, status int, message string) {
	ctx.JSON(status, gin.H{"code": http.StatusText(status), "message": message})
}
