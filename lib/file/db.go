package file

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	_ "github.com/go-sql-driver/mysql"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/crypt"
	"ehang.io/nps/lib/rate"
)

// DummyJsonDB 用于兼容旧代码，提供与JSON存储相关的接口，方法均为空实现
type DummyJsonDB struct {
	Clients sync.Map
	Tasks   sync.Map
	Hosts   sync.Map
	// 注意：Glob类型应由项目其他文件统一定义，此处不重复声明
	Global interface{}
}

func NewDummyJsonDB() *DummyJsonDB {
	return &DummyJsonDB{}
}

func (d *DummyJsonDB) LoadClientFromJsonFile() {}
func (d *DummyJsonDB) LoadTaskFromJsonFile()   {}
func (d *DummyJsonDB) LoadHostFromJsonFile()   {}
func (d *DummyJsonDB) LoadGlobalFromJsonFile() {}
func (d *DummyJsonDB) StoreClientsToJsonFile() {}
func (d *DummyJsonDB) StoreTasksToJsonFile()   {}
func (d *DummyJsonDB) StoreHostToJsonFile()    {}
func (d *DummyJsonDB) StoreGlobalToJsonFile()  {}

// 为兼容外部调用，增加以下方法（均返回0，可按实际需要扩展实现）
func (d *DummyJsonDB) GetTaskId() int   { return 0 }
func (d *DummyJsonDB) GetHostId() int   { return 0 }
func (d *DummyJsonDB) GetClientId() int { return 0 }

// 注意：Client、Tunnel、Host 等类型需在其它文件中定义，此处假设它们已存在并包含所使用的字段

// DbUtils 提供MySQL存储操作，同时保留 JsonDb 字段以兼容旧代码调用
type DbUtils struct {
	SqlDB  *sql.DB
	JsonDb *DummyJsonDB
}

var (
	Db   *DbUtils
	once sync.Once
)

// GetDb 建立 MySQL 数据库连接，并返回 DbUtils 实例
func GetDb() *DbUtils {
	once.Do(func() {
		// 请确保安装 MySQL 驱动：执行 go get github.com/go-sql-driver/mysql
		// 使用合适的 DSN 连接 MySQL，请根据实际情况修改用户名、密码、地址与数据库名称
		// Get the DSN from nps.conf configuration
		dsn := common.GetConfig("mysql_dsn")
		if dsn == "" {
			panic("mysql_dsn not found in nps.conf")
		}
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			panic(err)
		}
		// 可选：设置数据库连接池参数，例如 db.SetMaxOpenConns(x)
		Db = &DbUtils{
			SqlDB:  db,
			JsonDb: NewDummyJsonDB(), // 用于兼容旧调用
		}
	})
	return Db
}

// 辅助函数：根据排序键获取已排序的客户端ID列表（通过数据库查询，示例中假设表名为 clients）
func GetSortedClientIDs(orderBy string, order string) ([]int, error) {
	query := fmt.Sprintf("SELECT id FROM clients ORDER BY %s %s", orderBy, order)
	rows, err := GetDb().SqlDB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		keys = append(keys, id)
	}
	// 为防万一再做一次排序
	sort.Ints(keys)
	return keys, nil
}

// GetMapKeys 返回传入 sync.Map 中的所有键（假设键为 int 类型）。
func GetMapKeys(m sync.Map, isSort bool, sortKey, order string) []int {
	var keys []int
	m.Range(func(key, _ interface{}) bool {
		keys = append(keys, key.(int))
		return true
	})
	if isSort {
		sort.Ints(keys)
	}
	return keys
}

// GetClientList 从 MySQL 中按条件获取客户端列表及总数
// 修改为返回 ([]*Client, int) 两个值，错误通过 panic 抛出。
func (s *DbUtils) GetClientList(start, length int, search, sortField, order string, clientId int) ([]*Client, int) {
	where := "WHERE no_display = 0"
	if clientId != 0 {
		where += fmt.Sprintf(" AND id = %d", clientId)
	}
	if search != "" {
		// 假设在数据库中 search 对应 id、verify_key、remark 字段的匹配
		where += fmt.Sprintf(" AND (id = '%s' OR verify_key LIKE '%%%s%%' OR remark LIKE '%%%s%%')", search, search, search)
	}
	if sortField == "" {
		sortField = "id"
	}
	// 查询总数
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM clients %s", where)
	var cnt int
	if err := s.SqlDB.QueryRow(countQuery).Scan(&cnt); err != nil {
		panic(err)
	}
	// 查询数据
	query := fmt.Sprintf("SELECT id, verify_key, remark, IFNULL(inlet_flow, 0) FROM clients %s ORDER BY %s %s LIMIT %d, %d", where, sortField, order, start, length)
	rows, err := s.SqlDB.Query(query)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	var list []*Client
	for rows.Next() {
		var c Client
		var inletFlow int64
		if err := rows.Scan(&c.Id, &c.VerifyKey, &c.Remark, &inletFlow); err != nil {
			panic(err)
		}
		if c.Flow == nil {
			c.Flow = new(Flow)
		}
		c.Flow.InletFlow = inletFlow
		c.NowRate = 0 // 设置默认值，防止前端读取时出现 null
		list = append(list, &c)
	}
	return list, cnt
}

// GetIdByVerifyKey 根据 verify key 获取客户端 ID，并更新其地址信息
func (s *DbUtils) GetIdByVerifyKey(vKey string, addr string) (int, error) {
	// 这里通过 MySQL 中的 MD5 函数匹配 verify_key
	query := "SELECT id FROM clients WHERE MD5(verify_key) = ? AND status = 1 LIMIT 1"
	var id int
	if err := s.SqlDB.QueryRow(query, vKey).Scan(&id); err != nil {
		return 0, errors.New("not found")
	}
	// 更新地址信息
	ip := common.GetIpByAddr(addr)
	_, err := s.SqlDB.Exec("UPDATE clients SET addr = ? WHERE id = ?", ip, id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// NewTask 在数据库中创建新任务，并要求密钥在 secret 或 p2p 模式下唯一
func (s *DbUtils) NewTask(t *Tunnel) error {
	// 检查 secret 或 p2p 模式下的密码唯一性
	query := "SELECT COUNT(*) FROM tasks WHERE (mode = 'secret' OR mode = 'p2p') AND password = ?"
	var count int
	err := s.SqlDB.QueryRow(query, t.Password).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return errors.New(fmt.Sprintf("secret mode keys %s must be unique", t.Password))
	}
	// 插入任务记录，假设 tasks 表包含 id、password、mode 等字段
	_, err = s.SqlDB.Exec("INSERT INTO tasks (id, password, mode) VALUES (?, ?, ?)", t.Id, t.Password, t.Mode)
	return err
}

// UpdateTask 更新任务记录
func (s *DbUtils) UpdateTask(t *Tunnel) error {
	_, err := s.SqlDB.Exec("UPDATE tasks SET password = ?, mode = ? WHERE id = ?", t.Password, t.Mode, t.Id)
	return err
}

// SaveGlobal 保存全局配置信息
func (s *DbUtils) SaveGlobal(t *Glob) error {
	// 由于 Glob 类型不包含 Config 字段，此处暂存空值，或可自行转换 t 为字符串
	_, err := s.SqlDB.Exec("UPDATE globals SET config = ? LIMIT 1", "")
	return err
}

// DelTask 删除任务记录
func (s *DbUtils) DelTask(id int) error {
	_, err := s.SqlDB.Exec("DELETE FROM tasks WHERE id = ?", id)
	return err
}

// GetTaskByMd5Password 根据密码的 MD5 值获取任务记录
func (s *DbUtils) GetTaskByMd5Password(p string) *Tunnel {
	query := "SELECT id, password, mode FROM tasks"
	rows, err := s.SqlDB.Query(query)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var t Tunnel
		if err := rows.Scan(&t.Id, &t.Password, &t.Mode); err != nil {
			continue
		}
		if crypt.Md5(t.Password) == p {
			return &t
		}
	}
	return nil
}

// GetTask 根据任务 ID 获取任务记录
func (s *DbUtils) GetTask(id int) (*Tunnel, error) {
	query := "SELECT id, password, mode FROM tasks WHERE id = ? LIMIT 1"
	var t Tunnel
	if err := s.SqlDB.QueryRow(query, id).Scan(&t.Id, &t.Password, &t.Mode); err != nil {
		return nil, errors.New("not found")
	}
	return &t, nil
}

// DelHost 删除 host 记录
func (s *DbUtils) DelHost(id int) error {
	_, err := s.SqlDB.Exec("DELETE FROM hosts WHERE id = ?", id)
	return err
}

// IsHostExist 检查 host 是否已存在（排除自身记录）
func (s *DbUtils) IsHostExist(h *Host) bool {
	query := "SELECT COUNT(*) FROM hosts WHERE id <> ? AND host = ? AND location = ? AND (scheme = 'all' OR scheme = ?)"
	var count int
	err := s.SqlDB.QueryRow(query, h.Id, h.Host, h.Location, h.Scheme).Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

// NewHost 创建新的 host 记录
func (s *DbUtils) NewHost(t *Host) error {
	if t.Location == "" {
		t.Location = "/"
	}
	if s.IsHostExist(t) {
		return errors.New("host has exist")
	}
	_, err := s.SqlDB.Exec("INSERT INTO hosts (id, host, location, scheme) VALUES (?, ?, ?, ?)", t.Id, t.Host, t.Location, t.Scheme)
	return err
}

// GetHost 按条件获取 host 列表及总数
func (s *DbUtils) GetHost(start, length int, id int, search string) ([]*Host, int, error) {
	where := "WHERE 1=1"
	if search != "" {
		where += fmt.Sprintf(" AND (id = '%s' OR host LIKE '%%%s%%' OR remark LIKE '%%%s%%')", search, search, search)
	}
	if id != 0 {
		where += fmt.Sprintf(" AND client_id = %d", id)
	}
	countQuery := "SELECT COUNT(*) FROM hosts " + where
	var cnt int
	if err := s.SqlDB.QueryRow(countQuery).Scan(&cnt); err != nil {
		return nil, 0, err
	}
	query := fmt.Sprintf("SELECT id, host, location, scheme, remark FROM hosts %s LIMIT %d, %d", where, start, length)
	rows, err := s.SqlDB.Query(query)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []*Host
	for rows.Next() {
		var h Host
		if err := rows.Scan(&h.Id, &h.Host, &h.Location, &h.Scheme, &h.Remark); err != nil {
			return nil, 0, err
		}
		list = append(list, &h)
	}
	return list, cnt, nil
}

// DelClient 删除客户端记录
func (s *DbUtils) DelClient(id int) error {
	_, err := s.SqlDB.Exec("DELETE FROM clients WHERE id = ?", id)
	return err
}

// NewClient 创建新的客户端记录，并进行必要的检测与初始化
func (s *DbUtils) NewClient(c *Client) error {
	// 检查 web 登录用户名是否重复
	if c.WebUserName != "" && !s.VerifyUserName(c.WebUserName, c.Id) {
		return errors.New("web login username duplicate, please reset")
	}
	// 设置 VerifyKey 和 Rate
	if c.VerifyKey == "" {
		c.VerifyKey = crypt.GetVkey()
	}
	if c.RateLimit == 0 {
		c.Rate = rate.NewRate(int64(2 << 23))
	} else if c.Rate == nil {
		c.Rate = rate.NewRate(int64(c.RateLimit * 1024))
	}
	c.Rate.Start()
	if !s.VerifyVkey(c.VerifyKey, c.Id) {
		return errors.New("Vkey duplicate, please reset")
	}
	// 若 ID 由数据库自增生成, 此处无需赋值
	_, err := s.SqlDB.Exec("INSERT INTO clients (id, verify_key, web_user_name, rate_limit, remark) VALUES (?, ?, ?, ?, ?)", c.Id, c.VerifyKey, c.WebUserName, c.RateLimit, c.Remark)
	return err
}

// VerifyVkey 检查 VerifyKey 是否唯一
func (s *DbUtils) VerifyVkey(vkey string, id int) bool {
	query := "SELECT COUNT(*) FROM clients WHERE verify_key = ? AND id <> ?"
	var count int
	s.SqlDB.QueryRow(query, vkey, id).Scan(&count)
	return count == 0
}

// VerifyUserName 检查 Web 登录用户名是否唯一
func (s *DbUtils) VerifyUserName(username string, id int) bool {
	query := "SELECT COUNT(*) FROM clients WHERE web_user_name = ? AND id <> ?"
	var count int
	s.SqlDB.QueryRow(query, username, id).Scan(&count)
	return count == 0
}

// UpdateClient 更新客户端记录
func (s *DbUtils) UpdateClient(t *Client) error {
	query := "UPDATE clients SET verify_key = ?, web_user_name = ?, rate_limit = ?, remark = ? WHERE id = ?"
	_, err := s.SqlDB.Exec(query, t.VerifyKey, t.WebUserName, t.RateLimit, t.Remark, t.Id)
	if t.RateLimit == 0 {
		t.Rate = rate.NewRate(int64(2 << 23))
		t.Rate.Start()
	}
	return err
}

// IsPubClient 判断客户端是否为公开客户端
func (s *DbUtils) IsPubClient(id int) bool {
	client, err := s.GetClient(id)
	if err == nil {
		return client.NoDisplay
	}
	return false
}

// GetClient 根据 ID 获取客户端记录
func (s *DbUtils) GetClient(id int) (*Client, error) {
	query := "SELECT id, verify_key, web_user_name, rate_limit, remark, no_display FROM clients WHERE id = ? LIMIT 1"
	var c Client
	err := s.SqlDB.QueryRow(query, id).Scan(&c.Id, &c.VerifyKey, &c.WebUserName, &c.RateLimit, &c.Remark, &c.NoDisplay)
	if err != nil {
		return nil, errors.New("未找到客户端")
	}
	if c.Flow == nil {
		c.Flow = new(Flow)
	}
	c.NowRate = 0 // 设置默认值，避免前端读取时为 null
	return &c, nil
}

// GetGlobal 获取全局配置信息
func (s *DbUtils) GetGlobal() *Glob {
	// 外部 Glob 类型不包含 Config 字段，此处直接返回空的 Glob
	return &Glob{}
}

// GetClientIdByVkey 根据 verify_key 的 MD5 值获取客户端 ID
func (s *DbUtils) GetClientIdByVkey(vkey string) (int, error) {
	query := "SELECT id FROM clients WHERE MD5(verify_key) = ? LIMIT 1"
	var id int
	if err := s.SqlDB.QueryRow(query, vkey).Scan(&id); err != nil {
		return 0, errors.New("未找到客户端")
	}
	return id, nil
}

// GetHostById 根据 ID 获取 host 记录
func (s *DbUtils) GetHostById(id int) (*Host, error) {
	query := "SELECT id, host, location, scheme, remark FROM hosts WHERE id = ? LIMIT 1"
	var h Host
	if err := s.SqlDB.QueryRow(query, id).Scan(&h.Id, &h.Host, &h.Location, &h.Scheme, &h.Remark); err != nil {
		return nil, errors.New("The host could not be parsed")
	}
	return &h, nil
}

// GetInfoByHost 根据请求中的 host 与 URL 信息返回匹配的 host 记录
func (s *DbUtils) GetInfoByHost(host string, r *http.Request) (*Host, error) {
	ip := common.GetIpByAddr(host)
	query := "SELECT id, host, location, scheme, remark FROM hosts WHERE host = ? AND scheme IN (?, 'all') AND is_close = 0"
	rows, err := s.SqlDB.Query(query, ip, r.URL.Scheme)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var selected *Host
	for rows.Next() {
		var h Host
		if err := rows.Scan(&h.Id, &h.Host, &h.Location, &h.Scheme, &h.Remark); err != nil {
			continue
		}
		if h.Location == "" {
			h.Location = "/"
		}
		if strings.HasPrefix(r.RequestURI, h.Location) {
			if selected == nil || len(h.Location) > len(selected.Location) {
				selected = &h
			}
		}
	}
	if selected != nil {
		return selected, nil
	}
	return nil, errors.New("The host could not be parsed")
}
