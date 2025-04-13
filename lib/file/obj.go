package file

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ehang.io/nps/lib/rate"
	"github.com/pkg/errors"
)

type Flow struct {
	ExportFlow int64
	InletFlow  int64
	FlowLimit  int64
	sync.RWMutex
}

func (s *Flow) Add(in, out int64) {
	s.Lock()
	defer s.Unlock()
	s.InletFlow += int64(in)
	s.ExportFlow += int64(out)
}

type Config struct {
	U        string
	P        string
	Compress bool
	Crypt    bool
}

type Client struct {
	Cnf             *Config
	Id              int        //id
	AccountId       int        //账号
	VerifyKey       string     //verify key
	Addr            string     //the ip of client
	Remark          string     //remark
	Status          bool       //is allow connect
	IsConnect       bool       //is the client connect
	RateLimit       int        //rate /kb
	Flow            *Flow      //flow setting
	InletFlow       int64      // new field for inlet flow
	NowRate         int64      // new field for current rate
	Rate            *rate.Rate //rate limit
	NoStore         bool       //no store to file
	NoDisplay       bool       //no display on web
	MaxConn         int        //the max connection num of client allow
	NowConn         int32      //the connection num of now
	WebUserName     string     //the username of web login
	WebPassword     string     //the password of web login
	ConfigConnAllow bool       //is allow connected by config file
	MaxTunnelNum    int
	Version         string
	BlackIpList     []string
	CreateTime      string
	LastOnlineTime  string
	sync.RWMutex
}

type Account struct {
	Cnf             *Config
	ExpireTime      string
	Id              int        //id
	Remark          string     //remark
	Status          bool       //is allow connect
	RateLimit       int        //rate /kb
	Flow            *Flow      //flow setting
	InletFlow       int64      // new field for inlet flow
	NowRate         int64      // new field for current rate
	Rate            *rate.Rate //rate limit
	MaxConn         int        //the max connection num of client allow
	NowConn         int32      //the connection num of now
	WebUserName     string     //the username of web login
	WebPassword     string     //the password of web login
	ConfigConnAllow bool       //is allow connected by config file
	MaxTunnelNum    int
	BlackIpList     []string
	CreateTime      string
	LastOnlineTime  string
	sync.RWMutex
}

func NewAccount() *Account {
	return &Account{
		Cnf:       new(Config),
		Id:        0,
		Remark:    "",
		Status:    true,
		RateLimit: 0,
		Flow:      new(Flow),
		Rate:      nil,
		RWMutex:   sync.RWMutex{},
	}
}

func NewClient(vKey string, noStore bool, noDisplay bool) *Client {
	return &Client{
		Cnf:       new(Config),
		Id:        0,
		VerifyKey: vKey,
		Addr:      "",
		Remark:    "",
		Status:    true,
		IsConnect: false,
		RateLimit: 0,
		Flow:      new(Flow),
		Rate:      nil,
		NoStore:   noStore,
		RWMutex:   sync.RWMutex{},
		NoDisplay: noDisplay,
	}
}

func (s *Client) CutConn() {
	atomic.AddInt32(&s.NowConn, 1)
}

func (s *Client) AddConn() {
	atomic.AddInt32(&s.NowConn, -1)
}

func (s *Client) GetConn() bool {
	if s.MaxConn == 0 || int(s.NowConn) < s.MaxConn {
		s.CutConn()
		return true
	}
	return false
}

func (s *Client) HasTunnel(t *Tunnel) (exist bool) {
	tasks, err := GetDb().GetTasksByClientId(s.Id)
	if err != nil {
		return false
	}
	for _, v := range tasks {
		if v.Port == t.Port && t.Port != 0 {
			return true
		}
	}
	return false
}

func (s *Client) GetTunnelNum() (num int) {
	tasks, err := GetDb().GetTasksByClientId(s.Id)
	if err == nil {
		num += len(tasks)
	}

	hosts, err := GetDb().GetHostsByClientId(s.Id)
	if err == nil {
		num += len(hosts)
	}
	return
}

func (s *Client) HasHost(h *Host) bool {
	hosts, err := GetDb().GetHostsByClientId(s.Id)
	if err != nil {
		return false
	}
	for _, v := range hosts {
		if v.Host == h.Host && h.Location == v.Location {
			return true
		}
	}
	return false
}

type Tunnel struct {
	Id                    int
	AccountId             int
	ClientId              int
	Port                  int
	ServerIp              string
	ExternalServiceDomain string
	Mode                  string
	Status                bool
	RunStatus             bool
	Client                *Client
	Ports                 string
	Flow                  *Flow
	Password              string
	Remark                string
	TargetAddr            string
	NoStore               bool
	IsHttp                bool
	LocalPath             string
	StripPre              string
	Target                *Target
	Host                  string // host
	HeaderChange          string // header change
	HostChange            string // host change
	Location              string // url router
	Scheme                string // http https all
	CertFilePath          string
	KeyFilePath           string
	IsClose               bool
	AutoHttps             bool // 自动https
	MultiAccount          *MultiAccount
	Health
	sync.RWMutex
}

type Health struct {
	HealthCheckTimeout  int
	HealthMaxFail       int
	HealthCheckInterval int
	HealthNextTime      time.Time
	HealthMap           map[string]int
	HttpHealthUrl       string
	HealthRemoveArr     []string
	HealthCheckType     string
	HealthCheckTarget   string
	sync.RWMutex
}

type Host struct {
	Id           int
	Host         string //host
	HeaderChange string //header change
	HostChange   string //host change
	Location     string //url router
	Remark       string //remark
	Scheme       string //http https all
	CertFilePath string
	KeyFilePath  string
	NoStore      bool
	IsClose      bool
	AutoHttps    bool // 自动https
	Flow         *Flow
	Client       *Client
	Target       *Target //目标
	Health       `json:"-"`
	sync.RWMutex
}

type Target struct {
	nowIndex   int
	TargetStr  string
	TargetArr  []string
	LocalProxy bool
	sync.RWMutex
}

type MultiAccount struct {
	AccountMap map[string]string // multi account and pwd
}

type Order struct {
	OrderId               int64   `json:"order_id"`                // 订单ID，主键
	AppId                 string  `json:"app_id"`                  // 关联的应用ID
	OrderAmount           float64 `json:"order_amount"`            // 订单金额
	Flow                  float64 `json:"flow"`                    // 订单金额
	Months                int     `json:"months"`                  // 月数
	OrderStatus           string  `json:"order_status"`            // 订单状态
	PaymentType           string  `json:"payment_type"`            // 支付方式
	ExternalTransactionId string  `json:"external_transaction_id"` // 外部交易号
	CreatedAt             int64   `json:"created_at"`              // 创建时间
	AccountId             string  `json:"account_id"`              // 用户账号
	sync.RWMutex
}

func (s *Target) GetRandomTarget() (string, error) {
	if s.TargetArr == nil {
		s.TargetArr = strings.Split(s.TargetStr, "\n")
	}
	if len(s.TargetArr) == 1 {
		return s.TargetArr[0], nil
	}
	if len(s.TargetArr) == 0 {
		return "", errors.New("all inward-bending targets are offline")
	}
	s.Lock()
	defer s.Unlock()
	if s.nowIndex >= len(s.TargetArr)-1 {
		s.nowIndex = -1
	}
	s.nowIndex++
	return s.TargetArr[s.nowIndex], nil
}

type Glob struct {
	BlackIpList []string
	sync.RWMutex
}
