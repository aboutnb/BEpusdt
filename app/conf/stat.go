package conf

import (
	"fmt"
	"strconv"
	"sync"
	"time"
)

const maxRecords = 1000

type stat struct {
	mu      sync.RWMutex
	records []bool
	index   int // 当前位置
	total   int // 已记录总数
	succ    int // 成功记录数
}

type info struct {
	Block string `json:"block"`
	Succ  string `json:"succ"`
	Time  int64  `json:"time"`
}

type runtimeInfo struct {
	Head      int64
	Queue     int
	UpdatedAt int64
}

var (
	data         sync.Map // map[string]*stat
	last         sync.Map
	runtimeStats sync.Map
	lastMu       sync.Mutex
)

func RecordRuntime(net string, head int64, queue int) {
	runtimeStats.Store(net, runtimeInfo{Head: head, Queue: queue, UpdatedAt: time.Now().Unix()})
}

func GetNetworkRuntime(net string) (head int64, queue int, updatedAt int64, ok bool) {
	value, ok := runtimeStats.Load(net)
	if !ok {
		return 0, 0, 0, false
	}
	item := value.(runtimeInfo)
	return item.Head, item.Queue, item.UpdatedAt, true
}

func getStat(net string) *stat {
	val, _ := data.LoadOrStore(net, &stat{
		records: make([]bool, maxRecords),
	})
	return val.(*stat)
}

func RecordSuccess(net, block string) {
	s := getStat(net)
	s.mu.Lock()

	if s.total >= maxRecords && !s.records[s.index] {
		s.succ++
	} else if s.total < maxRecords {
		s.succ++
	}

	s.records[s.index] = true
	s.index = (s.index + 1) % maxRecords
	if s.total < maxRecords {
		s.total++
	}
	s.mu.Unlock()
	lastMu.Lock()
	latestBlock := block
	if value, ok := last.Load(net); ok {
		previous := value.(info)
		previousNumber, previousErr := strconv.ParseInt(previous.Block, 10, 64)
		currentNumber, currentErr := strconv.ParseInt(block, 10, 64)
		if previousErr == nil && currentErr == nil && previousNumber > currentNumber {
			latestBlock = previous.Block
		}
	}
	last.Store(net, info{Block: latestBlock, Succ: GetSuccessRate(net), Time: time.Now().Unix()})
	lastMu.Unlock()
}

func RecordFailure(net string) {
	s := getStat(net)
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.total >= maxRecords && s.records[s.index] {
		s.succ--
	}

	s.records[s.index] = false
	s.index = (s.index + 1) % maxRecords
	if s.total < maxRecords {
		s.total++
	}
}

func GetStats() map[string]info {
	var m = make(map[string]info)
	last.Range(func(k, v interface{}) bool {
		m[k.(string)] = v.(info)

		return true
	})

	return m
}

func GetNetworkStat(net string) (block, success string, timestamp int64, ok bool) {
	v, ok := last.Load(net)
	if !ok {
		return "", "", 0, false
	}
	i := v.(info)
	return i.Block, GetSuccessRate(net), i.Time, true
}

func GetSuccessRate(net string) string {
	s := getStat(net)
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.total == 0 {
		return "100.00%"
	}

	return fmt.Sprintf("%.2f%%", float64(s.succ)/float64(s.total)*100)
}
