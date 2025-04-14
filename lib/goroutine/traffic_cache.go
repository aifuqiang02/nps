package goroutine

import (
	"sync"
	"time"

	"ehang.io/nps/lib/file"
	"github.com/astaxie/beego/logs"
)

var (
	trafficManager = NewTrafficCacheManager()
	initOnce       sync.Once
)

type TrafficRecord struct {
	accumulatedBytes int64
	lastUpdatedTime  time.Time
}

type TrafficCacheManager struct {
	sync.Mutex
	records     map[int]*TrafficRecord
	flushTicker *time.Ticker
}

func NewTrafficCacheManager() *TrafficCacheManager {
	return &TrafficCacheManager{
		records:     make(map[int]*TrafficRecord),
		flushTicker: time.NewTicker(1 * time.Minute),
	}
}

func (tcm *TrafficCacheManager) AccumulateTrafficData(accountID int, bytes int64) {
	tcm.Lock()
	defer tcm.Unlock()

	if record, exists := tcm.records[accountID]; exists {
		record.accumulatedBytes += bytes
	} else {
		tcm.records[accountID] = &TrafficRecord{
			accumulatedBytes: bytes,
			lastUpdatedTime:  time.Now(),
		}
	}
}

func (tcm *TrafficCacheManager) ConditionalFlush() {
	tcm.Lock()
	defer tcm.Unlock()

	for accountID, record := range tcm.records {
		if record.accumulatedBytes >= 1<<20 { // 1MB
			tcm.flushTrafficDataToDB(accountID, record)
			delete(tcm.records, accountID)
		}
	}
}

func (tcm *TrafficCacheManager) flushTrafficDataToDB(accountID int, record *TrafficRecord) {
	db := file.GetDb()
	if err := db.AddTraffic(accountID, float64(record.accumulatedBytes)/(1<<20)); err != nil {
		logs.Error("Failed to flush traffic data for account %d: %v", accountID, err)
	}
}

func InitTrafficManager() {
	initOnce.Do(func() {
		go func() {
			for range trafficManager.flushTicker.C {
				trafficManager.ConditionalFlush()
			}
		}()
	})
}
