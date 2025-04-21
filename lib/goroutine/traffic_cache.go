package goroutine

import (
	"net/http"
	"sync"
	"time"

	"ehang.io/nps/lib/file"
	"github.com/astaxie/beego/logs"
)

var (
	TrafficManager = NewTrafficCacheManager()
	initOnce       sync.Once
)

type TrafficRecord struct {
	accumulatedBytes int64
	lastUpdatedTime  time.Time
}

type TrafficCacheManager struct {
	sync.Mutex
	records     map[int]*TrafficRecord
	accounts    map[int]*file.Account
	hosts       map[string]*file.Host
	flowLimits  map[int]int64 // 流量限制缓存
	flushTicker *time.Ticker
}

func NewTrafficCacheManager() *TrafficCacheManager {
	return &TrafficCacheManager{
		records:     make(map[int]*TrafficRecord),
		flowLimits:  make(map[int]int64),
		accounts:    make(map[int]*file.Account),
		hosts:       make(map[string]*file.Host),
		flushTicker: time.NewTicker(5 * time.Second),
	}
}

func (tcm *TrafficCacheManager) AccumulateTrafficData(accountID int, bytes int64) {
	if record, exists := tcm.records[accountID]; exists {
		record.accumulatedBytes += bytes
	} else {
		tcm.records[accountID] = &TrafficRecord{
			accumulatedBytes: bytes,
			lastUpdatedTime:  time.Now(),
		}
	}
}

func (tcm *TrafficCacheManager) GetFlowLimitFromCache(accountID int) int64 {
	if limit, exists := tcm.flowLimits[accountID]; exists {
		return limit
	}

	// 缓存中没有则从数据库获取
	db := file.GetDb()
	limit, err := db.GetAccountFlowLimit(accountID)
	if err != nil {
		logs.Error("Failed to get flow limit for account %d: %v", accountID, err)
		return 0
	}

	tcm.flowLimits[accountID] = limit
	return limit
}

// GetInfoByHost 根据请求中的 host 与 URL 信息返回匹配的 host 记录
func (tcm *TrafficCacheManager) GetInfoByHost(host string, r *http.Request) (*file.Host, error) {
	if h, exists := tcm.hosts[host]; exists {
		return h, nil
	}
	db := file.GetDb()
	h, err := db.GetInfoByHost(host, r)
	tcm.hosts[host] = h
	return h, err
}

func (tcm *TrafficCacheManager) updateFlowLimit(accountID int) {
	db := file.GetDb()
	account, err := db.GetAccountInfo(accountID)
	if err != nil {
		return
	}
	tcm.accounts[accountID] = account
	limit := account.Flow.FlowLimit * (1 << 20)

	tcm.flowLimits[accountID] = limit
}

func (tcm *TrafficCacheManager) ConditionalFlush() {
	// 检查并处理缓存数据
	for accountID, record := range tcm.records {
		// 更新当前accountID的流量限制
		tcm.updateFlowLimit(accountID)

		limit := tcm.flowLimits[accountID]
		// 如果缓存数据达到1MB或者账户流量已超限，则写入数据库
		if record.accumulatedBytes >= 1<<20 ||
			(limit > 0 && record.accumulatedBytes >= limit) {
			tcm.flushTrafficDataToDB(accountID, record)
			delete(tcm.records, accountID)
		}
	}
}

func (tcm *TrafficCacheManager) flushTrafficDataToDB(accountID int, record *TrafficRecord) {
	db := file.GetDb()
	// 将字节转换为KB (1<<10 = 1024)
	kb := float64(record.accumulatedBytes) / (1 << 10)
	if err := db.AddTraffic(accountID, -kb); err != nil {
		logs.Error("Failed to flush traffic data for account %d: %v", accountID, err)
	}
}

func InitTrafficManager() {
	initOnce.Do(func() {
		go func() {
			for range TrafficManager.flushTicker.C {
				TrafficManager.ConditionalFlush()
			}
		}()
	})
}
