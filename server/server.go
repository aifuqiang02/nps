package server

import (
	"errors"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ehang.io/nps/lib/version"

	"ehang.io/nps/bridge"
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/server/proxy"
	"ehang.io/nps/server/tool"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

var (
	Bridge  *bridge.Bridge
	RunList sync.Map // map[int]interface{}
)

func init() {
	RunList = sync.Map{}
}

// init task from db
func InitFromCsv() {
	// Add a public password
	if vkey := beego.AppConfig.String("public_vkey"); vkey != "" {
		c := file.NewAccount()
		c.WebUserName = beego.AppConfig.String("web_username")
		c.WebPassword = beego.AppConfig.String("web_password")
		file.GetDb().NewAccount(c)
		RunList.Store(c.Id, nil)
		//RunList[c.Id] = nil
	}
	// Initialize services in server-side files
	// Query all active tasks from MySQL
	tasks, err := file.GetDb().GetAllTasks()
	if err != nil {
		logs.Error("Failed to get tasks from MySQL:", err)
		return
	}
	for _, task := range tasks {
		if task.Status {
			AddTask(task)
		}
	}
}

// get bridge command
func DealBridgeTask() {
	for {
		select {
		case t := <-Bridge.OpenTask:
			AddTask(t)
		case t := <-Bridge.CloseTask:
			StopServer(t.Id)
		case id := <-Bridge.CloseClient:
			DelTunnelAndHostByClientId(id, true)
			if client, err := file.GetDb().GetClient(id); err == nil {
				if client.NoStore {
					file.GetDb().DelClient(id)
				}
			}
		case tunnel := <-Bridge.OpenTask:
			StartTask(tunnel.Id)
		case s := <-Bridge.SecretChan:
			logs.Trace("New secret connection, addr", s.Conn.Conn.RemoteAddr())
			if t := file.GetDb().GetTaskByMd5Password(s.Password); t != nil {
				if t.Status {
					go proxy.NewBaseServer(Bridge, t).DealClient(s.Conn, t.Client, t.Target.TargetStr, nil, common.CONN_TCP, nil, t.Flow, t.Target.LocalProxy, nil)
				} else {
					s.Conn.Close()
					logs.Trace("This key %s cannot be processed,status is close", s.Password)
				}
			} else {
				logs.Trace("This key %s cannot be processed", s.Password)
				s.Conn.Close()
			}
		}
	}
}

// start a new server
func StartNewServer(bridgePort int, cnf *file.Tunnel, bridgeType string, bridgeDisconnect int) {
	Bridge = bridge.NewTunnel(bridgePort, bridgeType, common.GetBoolByStr(beego.AppConfig.String("ip_limit")), RunList, bridgeDisconnect)
	go func() {
		if err := Bridge.StartTunnel(); err != nil {
			logs.Error("start server bridge error", err)
			os.Exit(0)
		}
	}()
	if p, err := beego.AppConfig.Int("p2p_port"); err == nil {
		go proxy.NewP2PServer(p).Start()
		go proxy.NewP2PServer(p + 1).Start()
		go proxy.NewP2PServer(p + 2).Start()
	}
	go DealBridgeTask()
	go dealClientFlow()
	if svr := NewMode(Bridge, cnf); svr != nil {
		if err := svr.Start(); err != nil {
			logs.Error(err)
		}
		RunList.Store(cnf.Id, svr)
		//RunList[cnf.Id] = svr
	} else {
		logs.Error("Incorrect startup mode %s", cnf.Mode)
	}
}

func dealClientFlow() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			dealClientData()
		}
	}
}

// new a server by mode name
func NewMode(Bridge *bridge.Bridge, c *file.Tunnel) proxy.Service {
	var service proxy.Service
	switch c.Mode {
	case "tcp", "file":
		service = proxy.NewTunnelModeServer(proxy.ProcessTunnel, Bridge, c)
	case "socks5":
		service = proxy.NewSock5ModeServer(Bridge, c)
	case "httpProxy":
		service = proxy.NewTunnelModeServer(proxy.ProcessHttp, Bridge, c)
	case "tcpTrans":
		service = proxy.NewTunnelModeServer(proxy.HandleTrans, Bridge, c)
	case "udp":
		service = proxy.NewUdpModeServer(Bridge, c)
	case "webServer":
		InitFromCsv()
		t := &file.Tunnel{
			Port:   0,
			Mode:   "httpHostServer",
			Status: true,
		}
		AddTask(t)
		service = proxy.NewWebServer(Bridge)
	case "httpHostServer":
		httpPort, _ := beego.AppConfig.Int("http_proxy_port")
		httpsPort, _ := beego.AppConfig.Int("https_proxy_port")
		useCache, _ := beego.AppConfig.Bool("http_cache")
		cacheLen, _ := beego.AppConfig.Int("http_cache_length")
		addOrigin, _ := beego.AppConfig.Bool("http_add_origin_header")
		service = proxy.NewHttp(Bridge, c, httpPort, httpsPort, useCache, cacheLen, addOrigin)
	}
	return service
}

// stop server
func StopServer(id int) error {
	//if v, ok := RunList[id]; ok {
	if v, ok := RunList.Load(id); ok {
		if svr, ok := v.(proxy.Service); ok {
			if err := svr.Close(); err != nil {
				return err
			}
			logs.Info("stop server id %d", id)
		} else {
			logs.Warn("stop server id %d error", id)
		}
		if t, err := file.GetDb().GetTask(id); err != nil {
			return err
		} else {
			t.Status = false
			logs.Info("close port %d,remark %s,client id %d,task id %d", t.Port, t.Remark, t.Client.Id, t.Id)
			file.GetDb().UpdateTask(t)
		}
		//delete(RunList, id)
		RunList.Delete(id)
		return nil
	}
	return errors.New("task is not running")
}

// add task
func AddTask(t *file.Tunnel) error {
	if t.Mode == "secret" || t.Mode == "p2p" {
		logs.Info("secret task %s start ", t.Remark)
		//RunList[t.Id] = nil
		RunList.Store(t.Id, nil)
		return nil
	}
	if b := tool.TestServerPort(t.Port, t.Mode); !b && t.Mode != "httpHostServer" {
		logs.Error("taskId %d start error port %d open failed", t.Id, t.Port)
		return errors.New("the port open error")
	}
	if minute, err := beego.AppConfig.Int("flow_store_interval"); err == nil && minute > 0 {
		go flowSession(time.Minute * time.Duration(minute))
	}
	if svr := NewMode(Bridge, t); svr != nil {
		logs.Info("tunnel task %s start mode：%s port %d", t.Remark, t.Mode, t.Port)
		//RunList[t.Id] = svr
		RunList.Store(t.Id, svr)
		go func() {
			if err := svr.Start(); err != nil {
				logs.Error("clientId %d taskId %d start error %s", t.Client.Id, t.Id, err)
				//delete(RunList, t.Id)
				RunList.Delete(t.Id)
				return
			}
		}()
	} else {
		return errors.New("the mode is not correct")
	}
	return nil
}

// start task
func StartTask(id int) error {
	if t, err := file.GetDb().GetTask(id); err != nil {
		return err
	} else {
		AddTask(t)
		t.Status = true
		file.GetDb().UpdateTask(t)
	}
	return nil
}

// delete task
func DelTask(id int) error {
	//if _, ok := RunList[id]; ok {
	if _, ok := RunList.Load(id); ok {
		if err := StopServer(id); err != nil {
			return err
		}
	}
	return file.GetDb().DelTask(id)
}

// get task list by page num
func GetTunnel(start, length int, typeVal string, clientId int, search string, sortField string, order string) ([]*file.Tunnel, int) {
	all_list := make([]*file.Tunnel, 0) // store all Tunnel
	list := make([]*file.Tunnel, 0)
	var cnt int
	tasks, err := file.GetDb().GetAllTasks()
	if err != nil {
		logs.Error("Failed to get tasks:", err)
		return nil, 0
	}

	// filter tasks
	for _, v := range tasks {
		// 确保Client、Target和Flow对象已初始化
		if v.Client == nil {
			v.Client = &file.Client{Id: v.ClientId}
		}
		if v.Target == nil {
			v.Target = &file.Target{TargetStr: ""}
		}
		if v.Flow == nil {
			v.Flow = &file.Flow{}
		}

		if (typeVal != "" && v.Mode != typeVal || (clientId != 0 && v.Client.Id != clientId)) || (typeVal == "" && clientId != v.Client.Id) {
			continue
		}
		all_list = append(all_list, v)
	}
	// sort by Id, Remark, TargetStr, Port, asc or desc
	if sortField == "Id" {
		if order == "asc" {
			sort.SliceStable(all_list, func(i, j int) bool { return all_list[i].Id < all_list[j].Id })
		} else {
			sort.SliceStable(all_list, func(i, j int) bool { return all_list[i].Id > all_list[j].Id })
		}
	} else if sortField == "ClientId" {
		if order == "asc" {
			sort.SliceStable(all_list, func(i, j int) bool { return all_list[i].Client.Id < all_list[j].Client.Id })
		} else {
			sort.SliceStable(all_list, func(i, j int) bool { return all_list[i].Client.Id > all_list[j].Client.Id })
		}
	} else if sortField == "Remark" {
		if order == "asc" {
			sort.SliceStable(all_list, func(i, j int) bool { return all_list[i].Remark < all_list[j].Remark })
		} else {
			sort.SliceStable(all_list, func(i, j int) bool { return all_list[i].Remark > all_list[j].Remark })
		}
	} else if sortField == "Client.VerifyKey" {
		if order == "asc" {
			sort.SliceStable(all_list, func(i, j int) bool { return all_list[i].Client.VerifyKey < all_list[j].Client.VerifyKey })
		} else {
			sort.SliceStable(all_list, func(i, j int) bool { return all_list[i].Client.VerifyKey > all_list[j].Client.VerifyKey })
		}
	} else if sortField == "Target" {
		if order == "asc" {
			sort.SliceStable(all_list, func(i, j int) bool { return all_list[i].Target.TargetStr < all_list[j].Target.TargetStr })
		} else {
			sort.SliceStable(all_list, func(i, j int) bool { return all_list[i].Target.TargetStr > all_list[j].Target.TargetStr })
		}
	}

	// search
	for _, v := range all_list {
		if (typeVal != "" && v.Mode != typeVal || (clientId != 0 && v.Client.Id != clientId)) || (typeVal == "" && clientId != v.Client.Id) {
			continue
		}
		if search != "" && !(v.Id == common.GetIntNoErrByStr(search) || v.Port == common.GetIntNoErrByStr(search) || strings.Contains(v.Password, search) || strings.Contains(v.Remark, search) || strings.Contains(v.Target.TargetStr, search)) {
			continue
		}
		cnt++
		if _, ok := Bridge.Client.Load(v.Client.Id); ok {
			v.Client.IsConnect = true
		} else {
			v.Client.IsConnect = false
		}
		if start--; start < 0 {
			if length--; length >= 0 {
				if _, ok := RunList.Load(v.Id); ok {
					v.RunStatus = true
				} else {
					v.RunStatus = false
				}
				list = append(list, v)
			}
		}
	}
	return list, cnt
}

// get task list by page num
func GetTunnelV2(start, length int, typeVal string, accountId int, clientId int, search string, sortField string, order string) ([]*file.Tunnel, int) {
	all_list := make([]*file.Tunnel, 0) // store all Tunnel
	list := make([]*file.Tunnel, 0)
	var cnt int
	tasks, err := file.GetDb().GetUserTasks(accountId, clientId)
	if err != nil {
		logs.Error("Failed to get tasks:", err)
		return nil, 0
	}
	logs.Error(" GetTunnelV2 tasks:", tasks)
	// filter tasks
	for _, v := range tasks {
		// 确保Client、Target和Flow对象已初始化
		if v.Client == nil {
			v.Client = &file.Client{Id: v.ClientId}
		}
		if v.Target == nil {
			v.Target = &file.Target{TargetStr: ""}
		}
		if v.Flow == nil {
			v.Flow = &file.Flow{}
		}
		all_list = append(all_list, v)
	}
	logs.Error(" GetTunnelV2 all_list:", all_list)
	// search
	for _, v := range all_list {
		cnt++
		if _, ok := Bridge.Client.Load(v.Client.Id); ok {
			v.Client.IsConnect = true
		} else {
			v.Client.IsConnect = false
		}
		if _, ok := RunList.Load(v.Id); ok {
			v.RunStatus = true
		} else {
			v.RunStatus = false
		}
		list = append(list, v)
	}
	logs.Error(" GetTunnelV2 list:", list)
	return list, cnt
}

// get client list
func GetClientList(start, length int, search, sort, order string, clientId int) (list []*file.Client, cnt int) {
	list, cnt = file.GetDb().GetClientList(start, length, search, sort, order, clientId)
	SetClientStatus(list)
	return
}

func dealClientData() {
	// 获取所有客户端数据
	clients, err := file.GetDb().GetAllClients()
	if err != nil {
		logs.Error("Failed to get clients from MySQL:", err)
		return
	}

	SetClientStatus(clients)
	logs.Debug(clients)
	logs.Debug("dealClientData")
}

func SetClientStatus(clients []*file.Client) {
	for _, v := range clients {
		if vv, ok := Bridge.Client.Load(v.Id); ok {
			v.IsConnect = true
			v.LastOnlineTime = time.Now().Format("2006-01-02 15:04:05")
			v.Version = vv.(*bridge.Client).Version
		} else {
			v.IsConnect = false
		}
	}
}

// delete all host and tasks by client id
func DelTunnelAndHostByClientId(clientId int, justDelNoStore bool) {
	var ids []int
	tasks, err := file.GetDb().GetAllTasks()
	if err != nil {
		logs.Error("Failed to get tasks for client deletion:", err)
		return
	}
	for _, v := range tasks {
		if justDelNoStore && !v.NoStore {
			continue
		}
		// 确保Client对象已初始化，但Host类型没有ClientId字段
		if v.Client == nil {
			v.Client = &file.Client{}
		}
		if v.Client.Id == clientId {
			ids = append(ids, v.Id)
		}
	}
	for _, id := range ids {
		DelTask(id)
	}
	ids = ids[:0]
	hosts, err := file.GetDb().GetAllHosts()
	if err != nil {
		logs.Error("Failed to get hosts for client deletion:", err)
		return
	}
	for _, v := range hosts {
		if justDelNoStore && !v.NoStore {
			continue
		}
		// 确保Client对象已初始化
		if v.Client == nil {
			v.Client = &file.Client{}
		}
		if v.Client.Id == clientId {
			ids = append(ids, v.Id)
		}
	}
	for _, id := range ids {
		file.GetDb().DelHost(id)
	}
}

// close the client
func DelClientConnect(clientId int) {
	Bridge.DelClient(clientId)
}

func GetDashboardData() map[string]interface{} {
	data := make(map[string]interface{})
	data["version"] = version.VERSION
	hosts, err := file.GetDb().GetAllHosts()
	if err != nil {
		logs.Error("Failed to get hosts count:", err)
		data["hostCount"] = 0
	} else {
		data["hostCount"] = len(hosts)
	}

	allClients, err := file.GetDb().GetAllClients()
	if err != nil {
		logs.Error("Failed to get clients count:", err)
		data["clientCount"] = 0
	} else {
		data["clientCount"] = len(allClients)
	}
	if beego.AppConfig.String("public_vkey") != "" { // remove public vkey
		data["clientCount"] = data["clientCount"].(int) - 1
	}
	dealClientData()
	c := 0
	var in, out int64
	if err != nil {
		logs.Error("Failed to get clients for dashboard:", err)
	} else {
		for _, v := range allClients {
			if v.IsConnect {
				c++
			}
			in += v.Flow.InletFlow
			out += v.Flow.ExportFlow
		}
	}
	data["clientOnlineCount"] = c
	data["inletFlowCount"] = int(in)
	data["exportFlowCount"] = int(out)
	var tcp, udp, secret, socks5, p2p, http int
	tasks, err := file.GetDb().GetAllTasks()
	if err == nil {
		for _, t := range tasks {
			switch t.Mode {
			case "tcp":
				tcp++
			case "socks5":
				socks5++
			case "httpProxy":
				http++
			case "udp":
				udp++
			case "p2p":
				p2p++
			case "secret":
				secret++
			}
		}
	}
	data["tcpC"] = tcp
	data["udpCount"] = udp
	data["socks5Count"] = socks5
	data["httpProxyCount"] = http
	data["secretCount"] = secret
	data["p2pCount"] = p2p
	data["bridgeType"] = beego.AppConfig.String("bridge_type")
	data["httpProxyPort"] = beego.AppConfig.String("http_proxy_port")
	data["httpsProxyPort"] = beego.AppConfig.String("https_proxy_port")
	data["ipLimit"] = beego.AppConfig.String("ip_limit")
	data["flowStoreInterval"] = beego.AppConfig.String("flow_store_interval")
	data["serverIp"] = beego.AppConfig.String("p2p_ip")
	data["p2pPort"] = beego.AppConfig.String("p2p_port")
	data["logLevel"] = beego.AppConfig.String("log_level")
	tcpCount := 0
	if err == nil {
		for _, c := range allClients {
			tcpCount += int(c.NowConn)
		}
	}
	data["tcpCount"] = tcpCount
	cpuPercet, _ := cpu.Percent(0, true)
	var cpuAll float64
	for _, v := range cpuPercet {
		cpuAll += v
	}
	loads, _ := load.Avg()
	data["load"] = loads.String()
	data["cpu"] = math.Round(cpuAll / float64(len(cpuPercet)))
	swap, _ := mem.SwapMemory()
	data["swap_mem"] = math.Round(swap.UsedPercent)
	vir, _ := mem.VirtualMemory()
	data["virtual_mem"] = math.Round(vir.UsedPercent)
	conn, _ := net.ProtoCounters(nil)
	io1, _ := net.IOCounters(false)
	time.Sleep(time.Millisecond * 500)
	io2, _ := net.IOCounters(false)
	if len(io2) > 0 && len(io1) > 0 {
		data["io_send"] = (io2[0].BytesSent - io1[0].BytesSent) * 2
		data["io_recv"] = (io2[0].BytesRecv - io1[0].BytesRecv) * 2
	}
	for _, v := range conn {
		data[v.Protocol] = v.Stats["CurrEstab"]
	}
	// chart
	var fg int
	if len(tool.ServerStatus) >= 10 {
		fg = len(tool.ServerStatus) / 10
		for i := 0; i <= 9; i++ {
			data["sys"+strconv.Itoa(i+1)] = tool.ServerStatus[i*fg]
		}
	}
	return data
}

// 定时更新流量数据到MySQL
func flowSession(m time.Duration) {
	ticker := time.NewTicker(m)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			// MySQL会自动持久化数据，这里只需要确保连接正常
			// 可以添加一些必要的定期更新逻辑
			logs.Info("Periodic data update check at", time.Now().Format("2006-01-02 15:04:05"))
		}
	}
}
