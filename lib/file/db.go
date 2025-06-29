package file

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/crypt"
	"ehang.io/nps/lib/rate"
	"github.com/astaxie/beego/logs"
	_ "github.com/go-sql-driver/mysql"
)

// DbUtils 提供MySQL存储操作
type DbUtils struct {
	SqlDB *sql.DB
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
			SqlDB: db,
		}
	})
	return Db
}

// 辅助函数：根据排序键获取已排序的客户端ID列表（通过数据库查询，示例中假设表名为 clients）
func GetSortedClientIDs(orderBy string, order string) ([]int, error) {
	query := fmt.Sprintf("SELECT id FROM clients ORDER BY %s %s", orderBy, order)
	fmt.Println("SQL Query:", query)
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
	fmt.Println("SQL Query for count:", countQuery)
	var cnt int
	if err := s.SqlDB.QueryRow(countQuery).Scan(&cnt); err != nil {
		panic(err)
	}
	// 查询数据
	query := fmt.Sprintf("SELECT id, verify_key, remark, IFNULL(inlet_flow, 0) as inlet_flow,status,account_id FROM clients %s ORDER BY %s %s LIMIT %d, %d", where, sortField, order, start, length)
	fmt.Println("SQL Query for data:", query)
	rows, err := s.SqlDB.Query(query)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	var list []*Client
	for rows.Next() {
		var c Client
		var inletFlow int64
		if err := rows.Scan(&c.Id, &c.VerifyKey, &c.Remark, &inletFlow, &c.Status, &c.AccountId); err != nil {
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
	query := "SELECT id FROM clients WHERE verify_key = ? AND status = 1 LIMIT 1"
	fmt.Println("SQL Query:", query, "with parameter:", vKey)
	var id int
	if err := s.SqlDB.QueryRow(query, vKey).Scan(&id); err != nil {
		return 0, errors.New("not found")
	}
	// 更新地址信息
	ip := common.GetIpByAddr(addr)
	updateQuery := "UPDATE clients SET addr = ? WHERE id = ?"
	fmt.Println("SQL Exec:", updateQuery, "with parameters:", ip, id)
	_, err := s.SqlDB.Exec(updateQuery, ip, id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// NewTask 在数据库中创建新任务，并要求密钥在 secret 或 p2p 模式下唯一
func (s *DbUtils) NewTask(t *Tunnel) error {
	// 检查 secret 或 p2p 模式下的密码唯一性
	query := "SELECT COUNT(*) FROM tasks WHERE (mode = 'secret' OR mode = 'p2p') AND password = ?"
	fmt.Println("SQL Query:", query, "with parameter:", t.Password)
	var count int
	err := s.SqlDB.QueryRow(query, t.Password).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return errors.New(fmt.Sprintf("secret mode keys %s must be unique", t.Password))
	}

	// 插入任务记录，使用完整的字段列表
	insertQuery := `INSERT INTO tasks (
		id, account_id, port, server_ip, mode, status, run_status, client_id, 
		ports, password, remark, target_addr, no_store, is_http, local_path, 
		strip_pre, header_change, host_change, location, host, scheme, 
		cert_file_path, key_file_path, is_close, auto_https, target, external_service_domain
	) VALUES (
		?, ?, ?, ?, ?, ?, ?, ?, 
		?, ?, ?, ?, ?, ?, ?, 
		?, ?, ?, ?, ?, ?, 
		?, ?, ?, ?, ?,?
	)`

	// 准备参数
	targetStr := ""
	if t.Target != nil {
		targetStr = t.Target.TargetStr
	}

	fmt.Println("SQL Exec:", insertQuery, "with parameters:", t.Id, t.AccountId, t.Port, t.ServerIp, t.Mode, t.Status, t.RunStatus, t.ClientId)
	_, err = s.SqlDB.Exec(
		insertQuery,
		t.Id, t.AccountId, t.Port, t.ServerIp, t.Mode, t.Status, t.RunStatus, t.ClientId,
		t.Ports, t.Password, t.Remark, t.TargetAddr, t.NoStore, t.IsHttp, t.LocalPath,
		t.StripPre, t.HeaderChange, t.HostChange, t.Location, t.Host, t.Scheme,
		t.CertFilePath, t.KeyFilePath, t.IsClose, t.AutoHttps, targetStr, t.ExternalServiceDomain,
	)
	return err
}

// UpdateTask 更新任务记录
func (s *DbUtils) UpdateTask(t *Tunnel) error {
	// 使用完整的字段列表更新任务记录
	updateQuery := `UPDATE tasks SET 
		 port = ?, server_ip = ?, mode = ?, status = ?, 
		ports = ?, password = ?, remark = ?, target_addr = ?, no_store = ?, is_http = ?, local_path = ?,
		strip_pre = ?, header_change = ?, host_change = ?, location = ?, host = ?, scheme = ?,
		cert_file_path = ?, key_file_path = ?, is_close = ?, auto_https = ?, target = ? , external_service_domain = ?
		WHERE id = ?`

	// 准备参数
	targetStr := ""
	if t.Target != nil {
		targetStr = t.Target.TargetStr
	}

	fmt.Println("SQL Exec:", updateQuery, "with parameters:", t.AccountId, t.Port, t.ServerIp, t.Mode, t.Status, t.RunStatus, t.ClientId)
	_, err := s.SqlDB.Exec(
		updateQuery,
		t.Port, t.ServerIp, t.Mode, t.Status,
		t.Ports, t.Password, t.Remark, t.TargetAddr, t.NoStore, t.IsHttp, t.LocalPath,
		t.StripPre, t.HeaderChange, t.HostChange, t.Location, t.Host, t.Scheme,
		t.CertFilePath, t.KeyFilePath, t.IsClose, t.AutoHttps, targetStr, t.ExternalServiceDomain,
		t.Id,
	)
	return err
}

// SaveGlobal 保存全局配置信息
func (s *DbUtils) SaveGlobal(t *Glob) error {
	updateQuery := "UPDATE globals SET config = ? LIMIT 1"
	fmt.Println("SQL Exec:", updateQuery, "with parameter:", "")
	_, err := s.SqlDB.Exec(updateQuery, "")
	return err
}

// DelTask 删除任务记录
func (s *DbUtils) DelTask(id int) error {
	delQuery := "DELETE FROM tasks WHERE id = ?"
	fmt.Println("SQL Exec:", delQuery, "with parameter:", id)
	_, err := s.SqlDB.Exec(delQuery, id)
	return err
}

// GetTaskByMd5Password 根据密码的 MD5 值获取任务记录
func (s *DbUtils) GetTaskByMd5Password(p string) *Tunnel {
	// 首先查询所有任务的ID、密码和模式
	query := "SELECT id, password, mode FROM tasks"
	fmt.Println("SQL Query:", query)
	rows, err := s.SqlDB.Query(query)
	if err != nil {
		return nil
	}
	defer rows.Close()

	// 遍历所有任务，查找匹配的密码
	for rows.Next() {
		var t Tunnel
		if err := rows.Scan(&t.Id, &t.Password, &t.Mode); err != nil {
			continue
		}

		// 如果找到匹配的密码，获取完整的任务记录
		if crypt.Md5(t.Password) == p {
			fullTask, err := s.GetTask(t.Id)
			if err != nil {
				return nil
			}
			return fullTask
		}
	}
	return nil
}

// GetTask 根据任务 ID 获取任务记录
func (s *DbUtils) GetTask(id int) (*Tunnel, error) {
	// 查询指定ID的任务记录，使用完整的字段列表
	query := `SELECT 
		id, account_id, port, server_ip, mode, status, run_status, client_id, 
		ports, password, remark, target_addr, no_store, is_http, local_path, 
		strip_pre, header_change, host_change, location, host, scheme, 
		cert_file_path, key_file_path, is_close, auto_https, IFNULL(target, '') as target,external_service_domain
		FROM tasks WHERE id = ? LIMIT 1`

	fmt.Println("SQL Query:", query, "with parameter:", id)

	var t Tunnel
	t.Target = &Target{}
	t.Flow = &Flow{}

	// 扫描所有字段
	if err := s.SqlDB.QueryRow(query, id).Scan(
		&t.Id, &t.AccountId, &t.Port, &t.ServerIp, &t.Mode, &t.Status, &t.RunStatus, &t.ClientId,
		&t.Ports, &t.Password, &t.Remark, &t.TargetAddr, &t.NoStore, &t.IsHttp, &t.LocalPath,
		&t.StripPre, &t.HeaderChange, &t.HostChange, &t.Location, &t.Host, &t.Scheme,
		&t.CertFilePath, &t.KeyFilePath, &t.IsClose, &t.AutoHttps, &t.Target.TargetStr, &t.ExternalServiceDomain,
	); err != nil {
		fmt.Println("GetTask err:", err)

		return nil, errors.New("not found")
	}

	// 初始化Client对象
	t.Client = &Client{Id: t.ClientId}
	t.Client.Cnf = &Config{}
	t.Client.Flow = &Flow{}
	return &t, nil
}

// DelHost 删除 host 记录
func (s *DbUtils) DelHost(id int) error {
	delQuery := "DELETE FROM tasks WHERE id = ?"
	fmt.Println("SQL Exec:", delQuery, "with parameter:", id)
	_, err := s.SqlDB.Exec(delQuery, id)
	return err
}

// IsHostExist 检查 host 是否已存在（排除自身记录）
func (s *DbUtils) IsHostExist(h *Host) bool {
	query := "SELECT COUNT(*) FROM tasks WHERE id <> ? AND host = ? AND location = ? AND (scheme = 'all' OR scheme = ?)"
	fmt.Println("SQL Query:", query, "with parameters:", h.Id, h.Host, h.Location, h.Scheme)
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

	// 插入host记录，使用完整的字段列表
	insertQuery := `INSERT INTO tasks (
		id, account_id, client_id, host, location, scheme, remark, 
		no_store, is_close, auto_https, target, status
	) VALUES (
		?, ?, ?, ?, ?, ?, ?, 
		?, ?, ?, ?, ?
	)`

	// 准备参数
	targetStr := ""
	if t.Target != nil {
		targetStr = t.Target.TargetStr
	}

	clientId := 0
	if t.Client != nil {
		clientId = t.Client.Id
	}

	// 默认值
	accountId := 0 // 可以从session中获取
	status := true // 默认启用

	fmt.Println("SQL Exec:", insertQuery, "with parameters:", t.Id, accountId, clientId, t.Host, t.Location, t.Scheme, t.Remark)
	_, err := s.SqlDB.Exec(
		insertQuery,
		t.Id, accountId, clientId, t.Host, t.Location, t.Scheme, t.Remark,
		t.NoStore, t.IsClose, t.AutoHttps, targetStr, status,
	)
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
	countQuery := "SELECT COUNT(*) FROM tasks " + where
	fmt.Println("SQL Query for count:", countQuery)
	var cnt int
	if err := s.SqlDB.QueryRow(countQuery).Scan(&cnt); err != nil {
		return nil, 0, err
	}
	query := fmt.Sprintf("SELECT id, host, location, scheme, remark,client_id,account_id FROM tasks %s LIMIT %d, %d", where, start, length)
	fmt.Println("SQL Query for data:", query)
	rows, err := s.SqlDB.Query(query)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []*Host
	for rows.Next() {
		var h Host
		h.Client = &Client{}
		if err := rows.Scan(&h.Id, &h.Host, &h.Location, &h.Scheme, &h.Remark, &h.Client.Id, &h.AccountId); err != nil {
			return nil, 0, err
		}
		list = append(list, &h)
	}
	return list, cnt, nil
}

// DelClient 删除客户端记录
func (s *DbUtils) DelClient(id int) error {
	delQuery := "DELETE FROM clients WHERE id = ?"
	fmt.Println("SQL Exec:", delQuery, "with parameter:", id)
	_, err := s.SqlDB.Exec(delQuery, id)
	return err
}

// NewClient 创建新的客户端记录，并进行必要的检测与初始化
func (s *DbUtils) NewClient(c *Client) error {
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
	insertQuery := "INSERT INTO clients (id, verify_key, account_id, rate_limit, remark) VALUES (?, ?, ?, ?, ?)"
	fmt.Println("SQL Exec:", insertQuery, "with parameters:", c.Id, c.VerifyKey, c.AccountId, c.RateLimit, c.Remark)
	_, err := s.SqlDB.Exec(insertQuery, c.Id, c.VerifyKey, c.AccountId, c.RateLimit, c.Remark)
	if err != nil {
		fmt.Println("NewClient err:", err)
	}
	return err
}

// VerifyVkey 检查 VerifyKey 是否唯一
func (s *DbUtils) VerifyVkey(vkey string, id int) bool {
	query := "SELECT COUNT(*) FROM clients WHERE verify_key = ? AND id <> ?"
	fmt.Println("SQL Query:", query, "with parameters:", vkey, id)
	var count int
	s.SqlDB.QueryRow(query, vkey, id).Scan(&count)
	return count == 0
}

// VerifyUserName 检查 Web 登录用户名是否唯一
func (s *DbUtils) VerifyUserName(username string, id int) bool {
	query := "SELECT COUNT(*) FROM accounts WHERE web_user_name = ? AND id <> ?"
	fmt.Println("SQL Query:", query, "with parameters:", username, id)
	var count int
	s.SqlDB.QueryRow(query, username, id).Scan(&count)
	return count == 0
}

// UpdateClient 更新客户端记录
func (s *DbUtils) UpdateClient(t *Client) error {
	query := "UPDATE clients SET verify_key = ?, web_user_name = ?, rate_limit = ?, remark = ? WHERE id = ?"
	fmt.Println("SQL Exec:", query, "with parameters:", t.VerifyKey, t.WebUserName, t.RateLimit, t.Remark, t.Id)
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
	fmt.Println("SQL Query:", query, "with parameter:", id)
	var c Client
	err := s.SqlDB.QueryRow(query, id).Scan(&c.Id, &c.VerifyKey, &c.WebUserName, &c.RateLimit, &c.Remark, &c.NoDisplay)
	if err != nil {
		return nil, errors.New("未找到客户端")
	}
	if c.Flow == nil {
		c.Flow = new(Flow)
	}
	c.NowRate = 0 // 设置默认值，避免前端读取时为 null
	c.Cnf = &Config{}
	return &c, nil
}

// GetGlobal 获取全局配置信息
func (s *DbUtils) GetGlobal() *Glob {
	// 外部 Glob 类型不包含 Config 字段，此处直接返回空的 Glob
	return &Glob{}
}

// GetNewClientId 获取新的客户端ID
func (s *DbUtils) GetNewClientId() int {
	query := "SELECT IFNULL(MAX(id), 0) + 1 FROM clients"
	var id int
	s.SqlDB.QueryRow(query).Scan(&id)
	return id
}

// GetNewTaskId 获取新的任务ID
func (s *DbUtils) GetNewTaskId() int {
	query := "SELECT IFNULL(MAX(id), 0) + 1 FROM tasks"
	var id int
	s.SqlDB.QueryRow(query).Scan(&id)
	return id
}

// GetNewHostId 获取新的Host ID
func (s *DbUtils) GetNewHostId() int {
	query := "SELECT IFNULL(MAX(id), 0) + 1 FROM tasks"
	var id int
	s.SqlDB.QueryRow(query).Scan(&id)
	return id
}

func (s *DbUtils) CreateOrder(order *Order) error {
	insertQuery := `INSERT INTO orders (
		app_id, order_amount, months, order_status, 
		payment_type, external_transaction_id,  account_id,flow
	) VALUES ( ?, ?, ?, ?, ?, ?,?,?)`

	_, err := s.SqlDB.Exec(
		insertQuery,
		order.AppId, order.OrderAmount, order.Months, order.OrderStatus,
		order.PaymentType, order.ExternalTransactionId, order.AccountId, order.Flow,
	)
	return err
}

func (s *DbUtils) GetOrderById(orderId int64) (*Order, error) {
	query := `SELECT 
		order_id, app_id, order_amount, months, order_status,
		payment_type, external_transaction_id, created_at, account_id
		FROM orders WHERE order_id = ? LIMIT 1`

	var order Order
	err := s.SqlDB.QueryRow(query, orderId).Scan(
		&order.OrderId, &order.AppId, &order.OrderAmount, &order.Months, &order.OrderStatus,
		&order.PaymentType, &order.ExternalTransactionId, &order.CreatedAt, &order.AccountId,
	)
	if err != nil {
		return nil, err
	}
	return &order, nil
}

func (s *DbUtils) GetNewOrderId() int64 {
	query := "SELECT IFNULL(MAX(order_id), 0) + 1 FROM orders"
	var id int64
	s.SqlDB.QueryRow(query).Scan(&id)
	return id
}

func (s *DbUtils) GetAllTasks() ([]*Tunnel, error) {
	// 查询所有任务记录，使用完整的字段列表
	query := `SELECT 
		id, account_id, port, server_ip, mode, status, run_status, client_id, 
		ports, password, remark, target_addr, no_store, is_http, local_path, 
		strip_pre, header_change, host_change, location, host, scheme, 
		cert_file_path, key_file_path, is_close, auto_https, IFNULL(target, '')
		FROM tasks WHERE status = 1`

	rows, err := s.SqlDB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*Tunnel
	for rows.Next() {
		var t Tunnel
		t.Target = &Target{}
		t.Flow = &Flow{}

		// 扫描所有字段
		if err := rows.Scan(
			&t.Id, &t.AccountId, &t.Port, &t.ServerIp, &t.Mode, &t.Status, &t.RunStatus, &t.ClientId,
			&t.Ports, &t.Password, &t.Remark, &t.TargetAddr, &t.NoStore, &t.IsHttp, &t.LocalPath,
			&t.StripPre, &t.HeaderChange, &t.HostChange, &t.Location, &t.Host, &t.Scheme,
			&t.CertFilePath, &t.KeyFilePath, &t.IsClose, &t.AutoHttps, &t.Target.TargetStr,
		); err != nil {
			return nil, err
		}

		// 初始化Client对象
		t.Client = &Client{Id: t.ClientId}
		t.Client.Flow = &Flow{}
		t.Client.Cnf = &Config{}

		tasks = append(tasks, &t)
	}
	return tasks, nil
}

func (s *DbUtils) GetUserTasks(accountId int, clientId int) ([]*Tunnel, error) {
	// 查询指定账户的任务记录，使用完整的字段列表
	query := `SELECT 
		id, account_id, port, server_ip, mode, status, run_status, client_id, 
		ports, password, remark, target_addr, no_store, is_http, local_path, 
		strip_pre, header_change, host_change, location, host, scheme, 
		cert_file_path, key_file_path, is_close, auto_https, IFNULL(target, '') as target,external_service_domain
		FROM tasks WHERE status = 1 AND account_id = ?`

	fmt.Println("SQL Query:", query, "with parameter:", accountId)
	rows, err := s.SqlDB.Query(query, accountId)
	if err != nil {
		fmt.Println("GetUserTasks err:", err)
		return nil, err
	}
	defer rows.Close()

	var tasks []*Tunnel
	for rows.Next() {
		var t Tunnel
		t.Target = &Target{}
		t.Flow = &Flow{}

		// 扫描所有字段
		if err := rows.Scan(
			&t.Id, &t.AccountId, &t.Port, &t.ServerIp, &t.Mode, &t.Status, &t.RunStatus, &t.ClientId,
			&t.Ports, &t.Password, &t.Remark, &t.TargetAddr, &t.NoStore, &t.IsHttp, &t.LocalPath,
			&t.StripPre, &t.HeaderChange, &t.HostChange, &t.Location, &t.Host, &t.Scheme,
			&t.CertFilePath, &t.KeyFilePath, &t.IsClose, &t.AutoHttps, &t.Target.TargetStr, &t.ExternalServiceDomain,
		); err != nil {
			return nil, err
		}

		// 初始化Client对象
		t.Client = &Client{Id: t.ClientId}

		tasks = append(tasks, &t)
	}
	return tasks, nil
}

func (s *DbUtils) GetByUsername(username string) (*Account, error) {
	query := "SELECT id, web_user_name, IFNULL(web_password, '') as web_password, IFNULL(nick_name, '') as nick_name, IFNULL(head_img_url, '') as head_img_url, rate_limit, remark FROM accounts WHERE status = 1 and web_user_name = ?"
	fmt.Println("SQL Query:", query, "with parameter:", username)
	var account Account
	err := s.SqlDB.QueryRow(query, username).Scan(&account.Id, &account.WebUserName, &account.WebPassword, &account.NickName, &account.HeadImgUrl, &account.RateLimit, &account.Remark)

	if err != nil {
		fmt.Println("GetByUsername err:", err)
		return nil, errors.New("账号不存在")
	}
	return &account, nil
}

func (s *DbUtils) GetByUsernameNoErr(username string) *Account {
	query := "SELECT id, web_user_name, IFNULL(web_password, '') as web_password, IFNULL(nick_name, '') as nick_name, IFNULL(head_img_url, '') as head_img_url, rate_limit, remark FROM accounts WHERE status = 1 and web_user_name = ?"
	fmt.Println("SQL Query:", query, "with parameter:", username)
	var account Account
	err := s.SqlDB.QueryRow(query, username).Scan(&account.Id, &account.WebUserName, &account.WebPassword, &account.NickName, &account.HeadImgUrl, &account.RateLimit, &account.Remark)

	if err != nil {
		fmt.Println("GetByUsername err:", err)
	}
	return &account
}

func (s *DbUtils) GetAllClients() ([]*Client, error) {
	query := "SELECT id, verify_key, web_user_name, IFNULL(web_password, ''), rate_limit, remark, no_display FROM clients WHERE status = 1"
	rows, err := s.SqlDB.Query(query)
	if err != nil {
		fmt.Println("GetAllClients err:", err)
		return nil, err
	}
	defer rows.Close()

	var clients []*Client
	for rows.Next() {
		var c Client
		var webPassword string
		if err := rows.Scan(&c.Id, &c.VerifyKey, &c.WebUserName, &webPassword, &c.RateLimit, &c.Remark, &c.NoDisplay); err != nil {
			return nil, err
		}
		c.WebPassword = webPassword
		if c.Flow == nil {
			c.Flow = new(Flow)
		}
		clients = append(clients, &c)
	}
	return clients, nil
}

// NewAccount 创建新的账户记录，并进行必要的检测与初始化
func (s *DbUtils) NewAccount(c *Account) error {
	// 检查 web 登录用户名是否重复
	if c.WebUserName != "" && !s.VerifyUserName(c.WebUserName, c.Id) {
		return errors.New("用户名已存在	")
	}
	if c.RateLimit == 0 {
		c.Rate = rate.NewRate(int64(2 << 23))
	} else if c.Rate == nil {
		c.Rate = rate.NewRate(int64(c.RateLimit * 1024))
	}
	c.Rate.Start()

	insertQuery := "INSERT INTO accounts (web_user_name, web_password,nick_name,head_img_url, rate_limit, remark) VALUES (?, ?, ?, ?, ?, ?)"
	fmt.Println("SQL Exec:", insertQuery, "with parameters:", c.WebUserName, c.WebPassword, c.NickName, c.HeadImgUrl, c.RateLimit, c.Remark)
	_, err := s.SqlDB.Exec(insertQuery, c.WebUserName, c.WebPassword, c.NickName, c.HeadImgUrl, c.RateLimit, c.Remark)
	return err
}

func (s *DbUtils) GetAllHosts() ([]*Host, error) {
	query := "SELECT id, host, location, scheme, remark, client_id, no_store FROM tasks WHERE status = 1"
	rows, err := s.SqlDB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []*Host
	for rows.Next() {
		var h Host
		h.Client = &Client{}
		if err := rows.Scan(&h.Id, &h.Host, &h.Location, &h.Scheme, &h.Remark, &h.Client.Id, &h.NoStore); err != nil {
			return nil, err
		}
		hosts = append(hosts, &h)
	}
	return hosts, nil
}

func (s *DbUtils) GetTasksByClientId(clientId int) ([]*Tunnel, error) {
	query := "SELECT id, port, mode, status, password, remark FROM tasks WHERE status = 1 AND client_id = ?"
	rows, err := s.SqlDB.Query(query, clientId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*Tunnel
	for rows.Next() {
		var t Tunnel
		if err := rows.Scan(&t.Id, &t.Port, &t.Mode, &t.Status, &t.Password, &t.Remark); err != nil {
			return nil, err
		}
		tasks = append(tasks, &t)
	}
	return tasks, nil
}

func (s *DbUtils) GetHostsByClientId(clientId int) ([]*Host, error) {
	query := "SELECT id, host, location, scheme, remark FROM tasks WHERE status = 1 AND client_id = ?"
	rows, err := s.SqlDB.Query(query, clientId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []*Host
	for rows.Next() {
		var h Host
		if err := rows.Scan(&h.Id, &h.Host, &h.Location, &h.Scheme, &h.Remark); err != nil {
			return nil, err
		}
		hosts = append(hosts, &h)
	}
	return hosts, nil
}

// GetClientIdByVkey 根据 verify_key 的 MD5 值获取客户端 ID
func (s *DbUtils) GetClientIdByVkey(vkey string) (int, error) {
	query := "SELECT id FROM clients WHERE verify_key = ? LIMIT 1"
	fmt.Println("SQL Query:", query, "with parameter:", vkey)
	var id int
	if err := s.SqlDB.QueryRow(query, vkey).Scan(&id); err != nil {
		return 0, errors.New("未找到客户端")
	}
	return id, nil
}

// GetClientIdByVkey 根据 verify_key 的 MD5 值获取客户端 ID
func (s *DbUtils) GetClientByVkeyAndAccountId(vkey string, accountId int) int {
	query := "SELECT id FROM clients WHERE verify_key = ? and account_id = ?  LIMIT 1"
	fmt.Println("SQL Query:", query, "with parameter:", vkey, accountId)
	var id int
	if err := s.SqlDB.QueryRow(query, vkey, accountId).Scan(&id); err != nil {
		return 0
	}
	return id
}

// GetHostById 根据 ID 获取 host 记录
func (s *DbUtils) GetHostById(id int) (*Host, error) {
	// 查询指定ID的host记录，使用完整的字段列表
	query := `SELECT 
		id, host, location, scheme, remark, client_id, 
		no_store, is_close, auto_https, IFNULL(target, '')
		FROM tasks WHERE id = ? LIMIT 1`

	fmt.Println("SQL Query:", query, "with parameter:", id)

	var h Host
	h.Target = &Target{}
	h.Flow = &Flow{}
	var clientId int

	// 扫描所有字段
	if err := s.SqlDB.QueryRow(query, id).Scan(
		&h.Id, &h.Host, &h.Location, &h.Scheme, &h.Remark, &clientId,
		&h.NoStore, &h.IsClose, &h.AutoHttps, &h.Target.TargetStr,
	); err != nil {
		return nil, errors.New("The host could not be parsed")
	}

	// 初始化Client对象
	h.Client = &Client{Id: clientId}

	return &h, nil
}

// GetInfoByHost 根据请求中的 host 与 URL 信息返回匹配的 host 记录
func (s *DbUtils) GetInfoByHost(host string, r *http.Request) (*Host, error) {
	ip := common.GetIpByAddr(host)

	// 查询匹配的host记录，使用更多字段
	query := `SELECT 
		id, host, location, scheme, remark, client_id, account_id,
		no_store, is_close, auto_https, IFNULL(target, '')
		FROM tasks t1 WHERE host = ? AND scheme IN (?, 'all') AND is_close = 0`

	fmt.Println("SQL Query1:", query, "with parameters:", ip, r.URL.Scheme)
	logs.Info("SQL Query2:%s %s %s  %s ", query, "with parameters:", ip, r.URL.Scheme)
	rows, err := s.SqlDB.Query(query, ip, r.URL.Scheme)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var selected *Host
	for rows.Next() {
		var h Host
		h.Target = &Target{}
		h.Flow = &Flow{}
		var clientId int
		var accountId int

		// 扫描所有字段
		if err := rows.Scan(
			&h.Id, &h.Host, &h.Location, &h.Scheme, &h.Remark, &clientId, &accountId,
			&h.NoStore, &h.IsClose, &h.AutoHttps, &h.Target.TargetStr,
		); err != nil {
			continue
		}
		// 初始化Client对象
		h.Client = &Client{Id: clientId}
		h.Client.Cnf = &Config{}
		h.Client.Flow = &Flow{}
		h.AccountId = accountId

		if h.Location == "" {
			h.Location = "/"
		}

		// 选择最匹配的记录
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

// GetOrderByExternalId 根据外部交易ID获取订单
func (s *DbUtils) GetOrderByExternalId(externalId string) (*Order, error) {
	query := `SELECT 
		order_id, app_id, order_amount, flow, months, order_status,
		payment_type, external_transaction_id, account_id
		FROM orders WHERE external_transaction_id = ? LIMIT 1`

	var order Order
	err := s.SqlDB.QueryRow(query, externalId).Scan(
		&order.OrderId, &order.AppId, &order.OrderAmount, &order.Flow, &order.Months, &order.OrderStatus,
		&order.PaymentType, &order.ExternalTransactionId, &order.AccountId,
	)
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// UpdateOrder 更新订单状态
func (s *DbUtils) UpdateOrder(order *Order) error {
	query := "UPDATE orders SET order_status = ? WHERE external_transaction_id = ?"
	_, err := s.SqlDB.Exec(query, order.OrderStatus, order.ExternalTransactionId)
	return err
}

// AddTraffic 给账号添加流量(KB)
func (s *DbUtils) AddTraffic(accountId int, flow float64) error {
	query := "UPDATE accounts SET flow = (CASE WHEN flow is null or flow <0 then 0 else flow end) + ? WHERE id = ?"
	_, err := s.SqlDB.Exec(query, flow, accountId) // 将GB转换为KB
	return err
}

// AddMonths 给账号添加月数
func (s *DbUtils) AddMonths(accountId int, months int) error {
	query := `UPDATE accounts SET expire_time = DATE_ADD(
		CASE 
			WHEN expire_time IS NULL OR expire_time < NOW() THEN NOW()
			ELSE expire_time 
		END, 
		INTERVAL ? MONTH
	) WHERE id = ?`
	_, err := s.SqlDB.Exec(query, months, accountId)
	return err
}

// GetAccountInfo 获取完整账户信息
func (s *DbUtils) GetAccountInfo(accountId int) (*Account, error) {
	query := "SELECT id, web_user_name, IFNULL(web_password, '') as web_password, flow, IFNULL(expire_time, '') as expire_time, rate_limit, remark FROM accounts WHERE id = ?"
	var account Account
	account.Flow = new(Flow) // 初始化Flow对象

	var flowStr, expireTimeStr string
	err := s.SqlDB.QueryRow(query, accountId).Scan(
		&account.Id,
		&account.WebUserName,
		&account.WebPassword,
		&flowStr,
		&expireTimeStr,
		&account.RateLimit,
		&account.Remark,
	)
	if err != nil {
		return nil, fmt.Errorf("获取账户信息失败: %v", err)
	}

	// 将字符串类型的flow转换为float64 (单位KB)
	flow, err := strconv.ParseFloat(flowStr, 64)
	if err != nil {
		return nil, fmt.Errorf("流量值转换失败: %v", err)
	}
	account.Flow.FlowLimit = int64(flow) // 转换为字节

	// 处理可能为空的expire_time
	if expireTimeStr != "" {
		account.ExpireTime = expireTimeStr
	}

	return &account, nil
}

func (s *DbUtils) GetAccountFlowLimit(accountID int) (int64, error) {
	query := "SELECT flow FROM accounts WHERE id = ?"
	var flowStr string
	err := s.SqlDB.QueryRow(query, accountID).Scan(&flowStr)
	if err != nil {
		return 0, err
	}

	// 处理空值情况
	if flowStr == "" {
		return 0, nil
	}

	// 解析字符串为float64
	flow, err := strconv.ParseFloat(flowStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse flow limit: %v", err)
	}

	// 转换为字节 (1MB = 1<<20 bytes)
	return int64(flow * (1 << 20)), nil
}

func (s *DbUtils) UpdateHost(h *Host) error {
	// 使用完整的字段列表更新host记录
	query := `UPDATE tasks SET 
		host = ?, location = ?, scheme = ?, remark = ?, client_id = ?,
		no_store = ?, is_close = ?, auto_https = ?, target = ?
		WHERE id = ?`

	// 准备参数
	targetStr := ""
	if h.Target != nil {
		targetStr = h.Target.TargetStr
	}

	clientId := 0
	if h.Client != nil {
		clientId = h.Client.Id
	}

	fmt.Println("SQL Exec:", query, "with parameters:", h.Host, h.Location, h.Scheme, h.Remark, clientId)

	tx, err := s.SqlDB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		query,
		h.Host, h.Location, h.Scheme, h.Remark, clientId,
		h.NoStore, h.IsClose, h.AutoHttps, targetStr,
		h.Id,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}
