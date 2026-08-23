package task

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/panjf2000/ants/v2"
	"github.com/shopspring/decimal"
	"github.com/smallnest/chanx"
	"github.com/spf13/cast"
	"github.com/tidwall/gjson"
	"github.com/v03413/bepusdt/app/conf"
	blockapi "github.com/v03413/bepusdt/app/core"
	"github.com/v03413/bepusdt/app/log"
	"github.com/v03413/bepusdt/app/model"
	"github.com/v03413/bepusdt/app/utils"
)

const (
	blockParseMaxNum = 10 // 每次解析区块的最大数量
	evmTransferEvent = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
)

var chainBlockNum sync.Map

type block struct {
	RollDelayOffset int64 // 延迟偏移量，某些RPC节点如果不延迟，会报错 block is out of range，目前发现 https://rpc.xlayer.tech/ 存在此问题
	ConfirmedOffset int   // 确认偏移量，开启交易确认后，区块高度需要减去此值认为交易已确认
}

type evmNative struct {
	Parse     bool
	Decimal   int32
	TradeType model.TradeType
}

type evm struct {
	Network          string
	Block            block
	Native           evmNative
	Client           *http.Client
	blockScanQueue   *chanx.UnboundedChan[evmBlock]
	LookbackInterval time.Duration // 回溯时每批入队的间隔，控制 RPC 调用速率；默认 500ms
	endpointMu       sync.Mutex
	endpointIndex    int
	eventMu          sync.Mutex
	eventLastRequest time.Time
}

type evmBlock struct {
	From int64
	To   int64
}

func (e *evm) syncBlocksForward(ctx context.Context) {
	if syncBreak(e.Network, e.blockScanQueue.Len()) {

		return
	}

	post := []byte(`{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`)
	endpoint := e.rpcEndpoint()
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(post))
	if err != nil {
		log.Task.Warn("Error creating request:", err)

		return
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := e.Client.Do(req)
	if err != nil {
		e.rotateEndpoint(endpoint)
		log.Task.Warn("Error sending request:", err)

		return
	}

	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		e.rotateEndpoint(endpoint)
		log.Task.Warn(fmt.Sprintf("EVM RPC HTTP error(%s): %d", e.Network, resp.StatusCode))
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		e.rotateEndpoint(endpoint)
		log.Task.Warn("Error reading response body:", err)

		return
	}

	var res = gjson.ParseBytes(body)
	if !res.IsObject() || res.Get("error").Exists() || res.Get("result").String() == "" {
		e.rotateEndpoint(endpoint)
		log.Task.Warn(fmt.Sprintf("EVM 数据解析错误(%s): %s", e.Network, string(body)))

		return
	}

	var now = utils.HexStr2Int(res.Get("result").String()).Int64() - e.Block.RollDelayOffset
	if now <= 0 {

		return
	}
	conf.RecordRuntime(e.Network, now, e.blockScanQueue.Len())

	var lastBlockNumber int64
	if v, ok := chainBlockNum.Load(e.Network); ok {
		lastBlockNumber = v.(int64)
	}

	if now-lastBlockNumber > cast.ToInt64(model.GetC(model.BlockHeightMaxDiff)) {

		lastBlockNumber = now - 1
	}

	chainBlockNum.Store(e.Network, now)
	if now <= lastBlockNumber {

		return
	}

	for from := lastBlockNumber + 1; from <= now; from += blockParseMaxNum {
		to := from + blockParseMaxNum - 1
		if to > now {
			to = now
		}

		e.blockScanQueue.In <- evmBlock{From: from, To: to}
	}
}

func (e *evm) lookbackBlocks(ctx context.Context) {
	if syncBreak(e.Network, e.blockScanQueue.Len()) {
		return
	}

	startAt, endAt, orderIDs, ok := getLookbackUnix(model.Network(e.Network))
	if !ok {
		return
	}
	if !e.hasNativeLookbackTargets() {
		if err := e.lookbackTokenTransfers(ctx, startAt, endAt); err != nil {
			log.Task.Warn(fmt.Sprintf("EVM token lookback failed(%s): %v", e.Network, err))
			return
		}
		markLookbackDone(orderIDs)
		return
	}

	interval := e.LookbackInterval
	if interval <= 0 {
		interval = time.Millisecond * 300
	}

	start, end := blockapi.New().GetBoundaryHeights(startAt, endAt, e.Network)
	for i := start; i <= end; i += blockParseMaxNum {
		select {
		case <-ctx.Done():
			return
		default:
		}
		for e.blockScanQueue.Len() >= blockQueueLimit {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
		to := i + blockParseMaxNum - 1
		if to > end {
			to = end
		}
		e.blockScanQueue.In <- evmBlock{From: i, To: to}
		time.Sleep(interval)
	}
	markLookbackDone(orderIDs)
}

func (e *evm) hasNativeLookbackTargets() bool {
	if !e.Native.Parse || e.Native.TradeType == "" || model.Db == nil {
		return false
	}
	var walletCount int64
	model.Db.Model(&model.Wallet{}).Where("trade_type = ?", e.Native.TradeType).Count(&walletCount)
	if walletCount > 0 {
		return true
	}
	var orderCount int64
	model.Db.Model(&model.Order{}).
		Where("trade_type = ? and status in (?)", e.Native.TradeType, receivableOrderStatuses()).
		Count(&orderCount)
	return orderCount > 0
}

func (e *evm) lookbackTokenTransfers(ctx context.Context, startAt, endAt int64) error {
	start, end := blockapi.New().GetBoundaryHeights(startAt, endAt, e.Network)
	const chunkSize int64 = 1000
	for from := start; from <= end; from += chunkSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		to := from + chunkSize - 1
		if to > end {
			to = end
		}
		transfers, err := e.parseEventTransfer(evmBlock{From: from, To: to}, nil)
		if err != nil {
			conf.RecordFailure(e.Network)
			return err
		}
		if len(transfers) == 0 {
			continue
		}
		if err := e.populateTransferTimestamps(ctx, transfers); err != nil {
			conf.RecordFailure(e.Network)
			return err
		}
		transferQueue.In <- transfers
	}
	return nil
}

func (e *evm) populateTransferTimestamps(ctx context.Context, transfers []transfer) error {
	blockTimes := make(map[int]time.Time)
	for _, item := range transfers {
		if _, ok := blockTimes[item.BlockNum]; ok {
			continue
		}
		post := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x%x",false],"id":1}`, item.BlockNum))
		endpoint := e.rpcEndpoint()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBuffer(post))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := e.Client.Do(req)
		if err != nil {
			e.rotateEndpoint(endpoint)
			return err
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		data := gjson.ParseBytes(body)
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices || data.Get("error").Exists() {
			e.rotateEndpoint(endpoint)
			return fmt.Errorf("eth_getBlockByNumber failed for lookback block %d", item.BlockNum)
		}
		timestamp := utils.HexStr2Int(data.Get("result.timestamp").String()).Int64()
		if timestamp <= 0 {
			return fmt.Errorf("missing timestamp for lookback block %d", item.BlockNum)
		}
		blockTimes[item.BlockNum] = time.Unix(timestamp, 0)
	}
	for index := range transfers {
		transfers[index].Timestamp = blockTimes[transfers[index].BlockNum]
	}
	return nil
}

func (e *evm) blockDispatch(ctx context.Context) {
	p, err := ants.NewPoolWithFunc(1, e.getBlockByNumber)
	if err != nil {
		log.Task.Warn("Error creating pool:", err)

		return
	}

	defer p.Release()

	for {
		select {
		case <-ctx.Done():
			return
		case n := <-e.blockScanQueue.Out:
			if err := p.Invoke(n); err != nil {
				e.scheduleBlockRetry(n)

				log.Task.Warn("Evm Block Dispatch Error invoking process block:", err)
			}
		}
	}
}

func (e *evm) getBlockByNumber(a any) {
	b, ok := a.(evmBlock)
	if !ok {
		log.Task.Warn("Evm Block Parse Error: expected []int64, got", a)

		return
	}

	items := make([]string, 0)
	for i := b.From; i <= b.To; i++ {
		items = append(items, fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x%x",%t],"id":%d}`, i, e.Native.Parse, i))
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	endpoint := e.rpcEndpoint()
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer([]byte(fmt.Sprintf(`[%s]`, strings.Join(items, ",")))))
	if err != nil {
		e.rotateEndpoint(endpoint)
		log.Task.Warn("Error creating request:", err)

		return
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := e.Client.Do(req)
	if err != nil {
		e.rotateEndpoint(endpoint)
		conf.RecordFailure(e.Network)
		e.scheduleBlockRetry(b)
		log.Task.Warn("eth_getBlockByNumber Error sending request:", err)

		return
	}

	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		e.rotateEndpoint(endpoint)
		conf.RecordFailure(e.Network)
		e.scheduleBlockRetry(b)
		log.Task.Warn(fmt.Sprintf("%s eth_getBlockByNumber HTTP %d", e.Network, resp.StatusCode))
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		e.rotateEndpoint(endpoint)
		conf.RecordFailure(e.Network)
		e.scheduleBlockRetry(b)
		log.Task.Warn("eth_getBlockByNumber Error reading response body:", err)

		return
	}

	parsedBlocks := gjson.ParseBytes(body)
	blockItems := parsedBlocks.Array()
	expectedBlocks := int(b.To - b.From + 1)
	if !parsedBlocks.IsArray() || len(blockItems) != expectedBlocks {
		e.rotateEndpoint(endpoint)
		conf.RecordFailure(e.Network)
		e.scheduleBlockRetry(b)
		log.Task.Warn(fmt.Sprintf("%s eth_getBlockByNumber incomplete batch: got %d want %d", e.Network, len(blockItems), expectedBlocks))
		return
	}

	nativeTransfers := make([]transfer, 0)
	blockTimestamp := make(map[string]time.Time)
	for _, itm := range blockItems {
		if itm.Get("error").Exists() {
			e.rotateEndpoint(endpoint)
			conf.RecordFailure(e.Network)
			e.scheduleBlockRetry(b)
			log.Task.Warn(fmt.Sprintf("%s eth_getBlockByNumber response error %s", e.Network, itm.Get("error").String()))

			return
		}

		timestamp := utils.HexStr2Int(itm.Get("result.timestamp").String()).Int64()
		blockTime := time.Unix(timestamp, 0)
		blockNumHex := itm.Get("result.number").String()
		blockTimestamp[blockNumHex] = blockTime

		var array = itm.Get("result.transactions").Array()
		if e.Native.Parse && len(array) != 0 {

			nativeTransfers = append(nativeTransfers, e.parseNativeTransfer(array, int(utils.HexStr2Int(blockNumHex).Int64()), blockTime)...)
		}
	}

	transfers, err := e.parseEventTransfer(b, blockTimestamp)
	if err != nil {
		conf.RecordFailure(e.Network)
		e.scheduleBlockRetry(b)
		log.Task.Warn("Evm Block Parse Error parsing block transfer:", err)

		return
	}
	conf.RecordSuccess(e.Network, cast.ToString(b.To))

	if len(nativeTransfers) > 0 {
		transferQueue.In <- nativeTransfers
	}
	if len(transfers) > 0 {
		transferQueue.In <- transfers
	}

	log.Task.Info(fmt.Sprintf("区块扫描完成(%s): %d → %d 成功率：%s", e.Network, b.From, b.To, conf.GetSuccessRate(e.Network)))
}

func (e *evm) parseNativeTransfer(array []gjson.Result, num int, timestamp time.Time) []transfer {
	nativeTransfers := make([]transfer, 0)
	for _, tx := range array {
		if tx.Get("input").String() != "0x" {
			// 非原生币交易

			continue
		}

		valStr := tx.Get("value").String()
		if valStr == "0x0" || len(valStr) < 3 {
			// 过滤 0 值交易

			continue
		}

		amount, ok := big.NewInt(0).SetString(valStr[2:], 16)
		if !ok || amount.Sign() <= 0 {

			continue
		}

		toAddress := tx.Get("to").String()
		if toAddress == "" { // 合约创建交易 to 为空

			continue
		}

		nativeTransfers = append(nativeTransfers, transfer{
			Network:     e.Network,
			FromAddress: tx.Get("from").String(),
			RecvAddress: toAddress,
			Amount:      decimal.NewFromBigInt(amount, e.Native.Decimal),
			TxHash:      tx.Get("hash").String(),
			BlockNum:    num,
			Timestamp:   timestamp,
			TradeType:   e.Native.TradeType,
		})
	}

	return nativeTransfers
}

func (e *evm) parseEventTransfer(b evmBlock, timestamp map[string]time.Time) ([]transfer, error) {
	e.eventMu.Lock()
	defer e.eventMu.Unlock()
	transfers := make([]transfer, 0)
	contracts := model.GetNetworkContracts(model.Network(e.Network))
	receivingTopics := e.receivingAddressTopics()
	if len(contracts) == 0 || len(receivingTopics) == 0 {
		return transfers, nil
	}
	topicsJSON, err := json.Marshal(receivingTopics)
	if err != nil {
		return transfers, fmt.Errorf("marshal receiving address topics: %w", err)
	}
	logs := make([]gjson.Result, 0)
	for _, contract := range contracts {
		e.waitForEventRPC()
		contractJSON, err := json.Marshal(contract)
		if err != nil {
			return transfers, fmt.Errorf("marshal contract address: %w", err)
		}
		post := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getLogs","params":[{"fromBlock":"0x%x","toBlock":"0x%x","address":%s,"topics":["%s",null,%s]}],"id":1}`, b.From, b.To, contractJSON, evmTransferEvent, topicsJSON))
		endpoint := e.rpcEndpoint()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBuffer(post))
		if err != nil {
			cancel()
			return transfers, fmt.Errorf("build eth_getLogs request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "BEpusdt-scanner/1.0")
		resp, err := e.Client.Do(req)
		if err != nil {
			cancel()
			e.rotateEndpoint(endpoint)
			return transfers, errors.Join(errors.New("eth_getLogs Post Error"), err)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			e.rotateEndpoint(endpoint)
			return transfers, fmt.Errorf("%s eth_getLogs HTTP %d", e.Network, resp.StatusCode)
		}
		if readErr != nil {
			e.rotateEndpoint(endpoint)
			return transfers, errors.Join(errors.New("eth_getLogs ReadAll Error"), readErr)
		}
		data := gjson.ParseBytes(body)
		if data.Get("error").Exists() {
			e.rotateEndpoint(endpoint)
			return transfers, fmt.Errorf("%s eth_getLogs response error %s", e.Network, data.Get("error").String())
		}
		if !data.Get("result").IsArray() {
			e.rotateEndpoint(endpoint)
			return transfers, fmt.Errorf("%s eth_getLogs returned an invalid result", e.Network)
		}
		logs = append(logs, data.Get("result").Array()...)
	}

	for _, itm := range logs {
		to := itm.Get("address").String()
		tradeType, ok := model.GetContractTrade(to)
		if !ok {

			continue
		}

		topics := itm.Get("topics").Array()
		if len(topics) < 3 {

			continue
		}

		if topics[0].String() != evmTransferEvent { // transfer event signature

			continue
		}

		fromTopic := topics[1].String()
		recvTopic := topics[2].String()
		dataHex := itm.Get("data").String()
		if len(fromTopic) < 66 || len(recvTopic) < 66 || len(dataHex) <= 2 || !strings.HasPrefix(dataHex, "0x") {
			continue
		}
		from := fmt.Sprintf("0x%s", fromTopic[len(fromTopic)-40:])
		recv := fmt.Sprintf("0x%s", recvTopic[len(recvTopic)-40:])
		amount, ok := big.NewInt(0).SetString(dataHex[2:], 16)
		if !ok || amount.Sign() <= 0 {

			continue
		}

		transfers = append(transfers, transfer{
			Network:     e.Network,
			FromAddress: from,
			RecvAddress: recv,
			Amount:      decimal.NewFromBigInt(amount, model.GetContractDecimal(to)),
			TxHash:      itm.Get("transactionHash").String(),
			BlockNum:    int(utils.HexStr2Int(itm.Get("blockNumber").String()).Int64()),
			Timestamp:   timestamp[itm.Get("blockNumber").String()],
			TradeType:   tradeType,
		})
	}

	return transfers, nil
}

func (e *evm) waitForEventRPC() {
	const minimumInterval = time.Second
	if wait := minimumInterval - time.Since(e.eventLastRequest); wait > 0 {
		time.Sleep(wait)
	}
	e.eventLastRequest = time.Now()
}

func (e *evm) scheduleBlockRetry(block evmBlock) {
	time.AfterFunc(2*time.Second, func() {
		if e.blockScanQueue.Len() < blockQueueLimit {
			e.blockScanQueue.In <- block
		}
	})
}

func (e *evm) receivingAddressTopics() []string {
	trades := model.GetNetworkTrades(model.Network(e.Network))
	if len(trades) == 0 || model.Db == nil {
		return nil
	}
	addresses := make([]string, 0)
	model.Db.Model(&model.Wallet{}).Where("trade_type in (?)", trades).Pluck("address", &addresses)
	var orderAddresses []string
	model.Db.Model(&model.Order{}).
		Where("trade_type in (?) and status in (?)", trades, receivableOrderStatuses()).
		Pluck("address", &orderAddresses)
	addresses = append(addresses, orderAddresses...)

	topics := make([]string, 0, len(addresses))
	seen := make(map[string]struct{}, len(addresses))
	for _, address := range addresses {
		address = strings.ToLower(strings.TrimSpace(address))
		if len(address) != 42 || !strings.HasPrefix(address, "0x") {
			continue
		}
		topic := "0x" + strings.Repeat("0", 24) + address[2:]
		if _, ok := seen[topic]; ok {
			continue
		}
		seen[topic] = struct{}{}
		topics = append(topics, topic)
	}
	return topics
}

func (e *evm) tradeConfirmHandle(ctx context.Context) {
	var orders = getConfirmingOrders(model.GetNetworkTrades(model.Network(e.Network)))
	var wg sync.WaitGroup

	var handle = func(o model.Order) {
		if model.GetC(model.BlockOffsetConfirm) == "1" {
			last, ok := chainBlockNum.Load(e.Network)
			if !ok {
				return
			}
			if cast.ToInt(last)-o.RefBlockNum < e.Block.ConfirmedOffset {
				return
			}
		}

		post := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getTransactionReceipt","params":["%s"],"id":1}`, o.RefHash))
		endpoint := e.rpcEndpoint()
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(post))
		if err != nil {
			log.Task.Warn("evm tradeConfirmHandle Error creating request:", err)

			return
		}

		req.Header.Set("Content-Type", "application/json")
		resp, err := e.Client.Do(req)
		if err != nil {
			e.rotateEndpoint(endpoint)
			log.Task.Warn("evm tradeConfirmHandle Error sending request:", err)

			return
		}

		defer resp.Body.Close()
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			e.rotateEndpoint(endpoint)
			log.Task.Warn(fmt.Sprintf("%s eth_getTransactionReceipt HTTP %d", e.Network, resp.StatusCode))
			return
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			e.rotateEndpoint(endpoint)
			log.Task.Warn("evm tradeConfirmHandle Error reading response body:", err)

			return
		}

		data := gjson.ParseBytes(body)
		if data.Get("error").Exists() {
			e.rotateEndpoint(endpoint)
			log.Task.Warn(fmt.Sprintf("%s eth_getTransactionReceipt response error %s", e.Network, data.Get("error").String()))

			return
		}

		if data.Get("result.status").String() == "0x1" {
			markFinalConfirmed(o)
		}
	}

	for _, order := range orders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			handle(order)
		}()
	}

	wg.Wait()
}

func (e *evm) rpcEndpoint() string {
	endpoints := e.rpcEndpoints()
	if len(endpoints) == 0 {
		return ""
	}
	e.endpointMu.Lock()
	defer e.endpointMu.Unlock()
	e.endpointIndex %= len(endpoints)
	return endpoints[e.endpointIndex]
}

func (e *evm) rpcEndpoints() []string {
	raw := strings.NewReplacer("\n", ",", "\r", ",").Replace(model.Endpoint(model.Network(e.Network)))
	parts := strings.Split(raw, ",")
	endpoints := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		endpoint := strings.TrimSpace(part)
		if endpoint == "" {
			continue
		}
		if _, ok := seen[endpoint]; ok {
			continue
		}
		seen[endpoint] = struct{}{}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints
}

func (e *evm) rotateEndpoint(failed string) {
	endpoints := e.rpcEndpoints()
	if len(endpoints) < 2 {
		return
	}
	e.endpointMu.Lock()
	defer e.endpointMu.Unlock()
	e.endpointIndex %= len(endpoints)
	if endpoints[e.endpointIndex] == failed {
		e.endpointIndex = (e.endpointIndex + 1) % len(endpoints)
		if log.Task != nil {
			log.Task.Warn(fmt.Sprintf("EVM RPC failover(%s): %s -> %s", e.Network, failed, endpoints[e.endpointIndex]))
		}
	}
}

func syncBreak(network string, num int) bool {
	if num >= blockQueueLimit {
		log.Task.Warn(fmt.Sprintf("%s 同步阻塞，当前区块消费堆积数量：%d", network, num))

		return true
	}

	if mqttSubscribed(network) {
		return false
	}

	trades := model.GetNetworkTrades(model.Network(network))
	if len(trades) == 0 {

		return true
	}

	var count int64
	model.Db.Model(&model.Wallet{}).
		// Payment scanners must stay active for enabled receiving wallets. The
		// other_notify flag only controls non-order transfer notifications.
		Where("status = ? and trade_type in (?)", model.WaStatusEnable, trades).
		Count(&count)
	if count > 0 {

		return false
	}

	return !hasLookbackOrders(trades)
}
