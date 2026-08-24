package merchant

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/v03413/bepusdt/app/model"
)

// API exposes the server-to-server contract used by Sub2API. Browser checkout
// endpoints remain separate and continue to use their existing protocol.
type API struct{}

var merchantBuildMu sync.Mutex

type capabilitiesRequest struct {
	Networks []string `json:"networks"`
}

type createOrderRequest struct {
	OrderID        string `json:"order_id"`
	Amount         string `json:"amount"`
	Fiat           string `json:"fiat"`
	TradeType      string `json:"trade_type"`
	NotifyURL      string `json:"notify_url"`
	RedirectURL    string `json:"redirect_url"`
	Name           string `json:"name"`
	TimeoutSeconds int64  `json:"timeout_seconds"`
	Rate           string `json:"rate"`
}

type queryOrderRequest struct {
	OrderID string `json:"order_id"`
	TradeID string `json:"trade_id"`
}

type networkSpec struct {
	Name      string
	TradeType model.TradeType
}

var networkSpecs = map[string]networkSpec{
	"tron": {Name: "Tron", TradeType: model.UsdtTrc20},
	"bsc":  {Name: "BSC", TradeType: model.UsdtBep20},
}

func (API) Capabilities(ctx *gin.Context) {
	var req capabilitiesRequest
	if !readAndVerifyJSON(ctx, &req) {
		return
	}
	networks := requestedNetworks(req.Networks)
	items := make([]gin.H, 0, len(networks))
	for _, network := range networks {
		spec := networkSpecs[network]
		wallets := model.GetAvailableWallets(spec.TradeType)
		rpc := strings.TrimSpace(model.GetC(rpcConfigKey(spec.TradeType)))
		rpcCount := endpointCount(rpc)
		ready := len(wallets) > 0 && rpcCount > 0
		reason := ""
		switch {
		case len(wallets) == 0:
			reason = "no enabled receiving wallet"
		case rpcCount == 0:
			reason = "no RPC endpoint configured"
		}
		items = append(items, gin.H{
			"crypto": "USDT", "network": network, "network_name": spec.Name,
			"trade_type": string(spec.TradeType), "wallet_count": len(wallets),
			"rpc_endpoint_set": rpcCount > 0, "rpc_endpoint_count": rpcCount,
			"scanner_block": "", "scanner_success": "", "last_scan_at": int64(0),
			"chain_head": int64(0), "scanner_lag": int64(0), "queue_depth": 0,
			"accepting_orders": ready, "reason": reason,
		})
	}
	ctx.JSON(http.StatusOK, gin.H{"code": "ok", "data": gin.H{"networks": items}})
}

func (API) Readiness(ctx *gin.Context) {
	if !readAndVerifyJSON(ctx, nil) {
		return
	}
	networks := requestedNetworks(nil)
	ready := false
	states := make([]gin.H, 0, len(networks))
	for _, network := range networks {
		spec := networkSpecs[network]
		walletCount := len(model.GetAvailableWallets(spec.TradeType))
		rpcCount := endpointCount(model.GetC(rpcConfigKey(spec.TradeType)))
		accepting := walletCount > 0 && rpcCount > 0
		ready = ready || accepting
		states = append(states, gin.H{"network": network, "accepting_orders": accepting, "wallet_count": walletCount, "rpc_endpoint_count": rpcCount})
	}
	ctx.JSON(http.StatusOK, gin.H{"code": "ok", "data": gin.H{"ready": ready, "networks": states, "checked_at": time.Now().Unix()}})
}

func (API) CreateOrder(ctx *gin.Context) {
	var req createOrderRequest
	if !readAndVerifyJSON(ctx, &req) {
		return
	}
	if err := validateCreateRequest(req); err != nil {
		merchantError(ctx, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	amount, _ := decimal.NewFromString(req.Amount)
	tradeType := model.TradeType(strings.ToLower(strings.TrimSpace(req.TradeType)))
	merchantBuildMu.Lock()
	defer merchantBuildMu.Unlock()

	var existing model.Order
	query := model.Db.Where("order_id = ?", strings.TrimSpace(req.OrderID)).Order("id desc").Limit(1).Find(&existing)
	if query.Error != nil {
		merchantError(ctx, http.StatusInternalServerError, "database_error", "could not load existing merchant order")
		return
	}
	if query.RowsAffected > 0 {
		if existing.ApiType != model.OrderApiTypeMerchant || !sameMerchantRequest(existing, req, amount, tradeType) {
			merchantError(ctx, http.StatusConflict, "idempotency_conflict", "order_id is already bound to different payment details")
			return
		}
		merchantSuccess(ctx, existing)
		return
	}

	order, err := model.StartBuildOrder(model.OrderParams{
		Money: amount, ApiType: model.OrderApiTypeMerchant, OrderId: strings.TrimSpace(req.OrderID),
		TradeType: tradeType, RedirectUrl: strings.TrimSpace(req.RedirectURL), NotifyUrl: strings.TrimSpace(req.NotifyURL),
		Name: strings.TrimSpace(req.Name), Timeout: req.TimeoutSeconds, Rate: strings.TrimSpace(req.Rate), Fiat: model.Fiat(strings.ToUpper(strings.TrimSpace(req.Fiat))),
	})
	if err != nil {
		merchantError(ctx, http.StatusUnprocessableEntity, "order_create_failed", err.Error())
		return
	}
	merchantSuccess(ctx, order)
}

func (API) QueryOrder(ctx *gin.Context) {
	var req queryOrderRequest
	if !readAndVerifyJSON(ctx, &req) {
		return
	}
	if strings.TrimSpace(req.TradeID) == "" || strings.TrimSpace(req.OrderID) == "" {
		merchantError(ctx, http.StatusBadRequest, "invalid_request", "order_id and trade_id are required")
		return
	}
	order, ok := model.GetTradeOrder(strings.TrimSpace(req.TradeID))
	if !ok || order.ApiType != model.OrderApiTypeMerchant || order.OrderId != strings.TrimSpace(req.OrderID) {
		merchantError(ctx, http.StatusNotFound, "order_not_found", "merchant order was not found")
		return
	}
	merchantSuccess(ctx, order)
}

func readAndVerifyJSON(ctx *gin.Context, target any) bool {
	raw, err := io.ReadAll(io.LimitReader(ctx.Request.Body, maxBodySize+1))
	if err != nil || len(raw) > maxBodySize || !verifyRequest(ctx, raw) {
		merchantError(ctx, http.StatusUnauthorized, "unauthorized", "invalid merchant signature")
		return false
	}
	if target == nil || len(raw) == 0 {
		return true
	}
	if err := json.Unmarshal(raw, target); err != nil {
		merchantError(ctx, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return false
	}
	return true
}

func merchantSuccess(ctx *gin.Context, order model.Order) {
	ctx.JSON(http.StatusOK, gin.H{"code": "ok", "data": merchantOrder(order)})
}

func merchantOrder(order model.Order) gin.H {
	statusName := merchantStatusName(order.Status)
	network := merchantNetwork(order.TradeType)
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("BEPUSDT_PUBLIC_BASE_URL")), "/")
	paymentURL := ""
	if base != "" {
		paymentURL = base + "/pay/checkout/" + url.PathEscape(order.TradeId)
	}
	return gin.H{
		"order_id": order.OrderId, "trade_id": order.TradeId, "status": order.Status, "status_name": statusName,
		"fiat": string(order.Fiat), "amount": order.Money, "crypto": string(order.Crypto), "actual_amount": order.Amount,
		"exchange_rate": order.Rate, "trade_type": string(order.TradeType), "network": network,
		"token": order.Address, "block_transaction_id": transactionHash(order), "transfer_at": transferAt(order),
		"created_at": order.CreatedAt.Time().Unix(), "expires_at": order.ExpiredAt.Unix(), "payment_url": paymentURL,
		"confirmation": gin.H{"confirmed": order.Status == model.OrderStatusSuccess, "block_number": order.RefBlockNum},
	}
}

func validateCreateRequest(req createOrderRequest) error {
	if strings.TrimSpace(req.OrderID) == "" || len(strings.TrimSpace(req.OrderID)) > 128 {
		return fmt.Errorf("order_id is required and must be at most 128 characters")
	}
	amount, err := decimal.NewFromString(strings.TrimSpace(req.Amount))
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("amount must be a positive decimal")
	}
	if !strings.EqualFold(strings.TrimSpace(req.Fiat), string(model.CNY)) {
		return fmt.Errorf("only CNY settlement is supported")
	}
	tradeType := model.TradeType(strings.ToLower(strings.TrimSpace(req.TradeType)))
	if !isMerchantTradeType(tradeType) {
		return fmt.Errorf("unsupported USDT network")
	}
	if !allowedMerchantURL(req.NotifyURL, "BEPUSDT_NOTIFY_HOSTS") || !allowedMerchantURL(req.RedirectURL, "BEPUSDT_REDIRECT_HOSTS") {
		return fmt.Errorf("callback or redirect URL host is not allowed")
	}
	if req.TimeoutSeconds < 180 || req.TimeoutSeconds > 3600 {
		return fmt.Errorf("timeout_seconds must be between 180 and 3600")
	}
	if rate, err := decimal.NewFromString(strings.TrimSpace(req.Rate)); err != nil || rate.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("rate must be a positive decimal")
	}
	return nil
}

func sameMerchantRequest(order model.Order, req createOrderRequest, amount decimal.Decimal, tradeType model.TradeType) bool {
	storedAmount, err := decimal.NewFromString(order.Money)
	return err == nil && storedAmount.Equal(amount) && order.Fiat == model.Fiat(strings.ToUpper(strings.TrimSpace(req.Fiat))) &&
		order.TradeType == tradeType && order.NotifyUrl == strings.TrimSpace(req.NotifyURL) && order.ReturnUrl == strings.TrimSpace(req.RedirectURL)
}

func allowedMerchantURL(raw, envName string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return false
	}
	allowed := strings.Split(os.Getenv(envName), ",")
	for _, host := range allowed {
		if strings.EqualFold(strings.TrimSpace(host), parsed.Hostname()) {
			return true
		}
	}
	return false
}

func requestedNetworks(input []string) []string {
	set := map[string]struct{}{}
	for _, network := range input {
		network = strings.ToLower(strings.TrimSpace(network))
		if _, ok := networkSpecs[network]; ok {
			set[network] = struct{}{}
		}
	}
	if len(set) == 0 {
		for network := range networkSpecs {
			set[network] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for network := range set {
		result = append(result, network)
	}
	sort.Strings(result)
	return result
}

func rpcConfigKey(tradeType model.TradeType) model.ConfKey {
	if tradeType == model.UsdtTrc20 {
		return model.RpcEndpointTron
	}
	return model.RpcEndpointBsc
}

func endpointCount(raw string) int {
	count := 0
	for _, endpoint := range strings.Split(raw, ",") {
		if strings.TrimSpace(endpoint) != "" {
			count++
		}
	}
	return count
}

func isMerchantTradeType(tradeType model.TradeType) bool {
	for _, spec := range networkSpecs {
		if spec.TradeType == tradeType {
			return true
		}
	}
	return false
}

func merchantNetwork(tradeType model.TradeType) string {
	for network, spec := range networkSpecs {
		if spec.TradeType == tradeType {
			return network
		}
	}
	return ""
}

func merchantStatusName(status int) string {
	switch status {
	case model.OrderStatusSuccess:
		return "succeeded"
	case model.OrderStatusConfirming:
		return "confirming"
	case model.OrderStatusExpired:
		return "expired"
	case model.OrderStatusCanceled:
		return "cancelled"
	case model.OrderStatusFailed:
		return "failed"
	default:
		return "waiting"
	}
}

func transactionHash(order model.Order) string {
	if order.Status == model.OrderStatusSuccess || order.Status == model.OrderStatusConfirming {
		return order.RefHash
	}
	return ""
}

func transferAt(order model.Order) int64 {
	if order.ConfirmedAt == nil || order.ConfirmedAt.Unix() <= 0 {
		return 0
	}
	return order.ConfirmedAt.Unix()
}

func merchantError(ctx *gin.Context, status int, code, message string) {
	ctx.JSON(status, gin.H{"code": code, "message": message})
}
