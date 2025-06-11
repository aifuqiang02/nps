package controllers

import (
	"strings"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/rate"
	"ehang.io/nps/server"
	"github.com/astaxie/beego"
)

type ClientController struct {
	BaseController
}

func (s *ClientController) List() {
	if s.Ctx.Request.Method == "GET" {
		s.Data["menu"] = "client"
		s.SetInfo("client")
		s.display("client/list")
		return
	}
	start, length := s.GetAjaxParams()
	var clientId = 0
	list, cnt := server.GetClientList(start, length, s.getEscapeString("search"), s.getEscapeString("sort"), s.getEscapeString("order"), clientId)
	cmd := make(map[string]interface{})
	ip := s.Ctx.Request.Host
	cmd["ip"] = common.GetIpByAddr(ip)
	cmd["bridgeType"] = beego.AppConfig.String("bridge_type")
	cmd["bridgePort"] = server.Bridge.TunnelPort
	s.AjaxTable(list, cnt, cnt, cmd)
}

// 添加客户端
func (s *ClientController) Add() {
	if s.Ctx.Request.Method == "GET" {
		s.Data["menu"] = "client"
		s.SetInfo("add client")
		s.display()
	} else {
		accountId := s.GetSessionIntNoErr("accountId")
		vkey := s.getEscapeString("vkey")
		clientId := file.GetDb().GetClientByVkeyAndAccountId(vkey, accountId)
		if clientId == 0 {
			clientId = int(file.GetDb().GetNewClientId())
			t := &file.Client{
				VerifyKey: vkey,
				Id:        clientId,
				AccountId: accountId,
				Status:    true,
				Remark:    s.getEscapeString("remark"),
				Cnf: &file.Config{
					U:        s.getEscapeString("u"),
					P:        s.getEscapeString("p"),
					Compress: common.GetBoolByStr(s.getEscapeString("compress")),
					Crypt:    s.GetBoolNoErr("crypt"),
				},
				ConfigConnAllow: s.GetBoolNoErr("config_conn_allow"),
				RateLimit:       s.GetIntNoErr("rate_limit"),
				MaxConn:         s.GetIntNoErr("max_conn"),
				MaxTunnelNum:    s.GetIntNoErr("max_tunnel"),
				Flow: &file.Flow{
					ExportFlow: 0,
					InletFlow:  0,
					FlowLimit:  int64(s.GetIntNoErr("flow_limit")),
				},
				BlackIpList: RemoveRepeatedElement(strings.Split(s.getEscapeString("blackiplist"), "\r\n")),
			}
			if err := file.GetDb().NewClient(t); err != nil {

				s.AjaxErr(err.Error())
			}
		}

		// Restart all TCP tasks for this client
		tasks, _ := file.GetDb().GetTasksByClientId(clientId)
		for _, task := range tasks {
			if task.Mode == "tcp" {
				server.StopServer(task.Id)
				server.StartTask(task.Id)
			}
		}

		rs := make(map[string]interface{})
		data := make(map[string]interface{})
		data["clientId"] = clientId
		data["vkey"] = vkey
		data["server"] = beego.AppConfig.String("external_service_ip") + ":" + beego.AppConfig.String("bridge_port")
		rs["code"] = 200
		rs["data"] = data
		s.Data["json"] = rs
		s.ServeJSON()
		s.StopRun()
	}
}
func (s *ClientController) GetClient() {
	if s.Ctx.Request.Method == "POST" {
		id := s.GetIntNoErr("id")
		data := make(map[string]interface{})
		if c, err := file.GetDb().GetClient(id); err != nil {
			data["code"] = 400
		} else {
			data["code"] = 200
			data["data"] = c
		}
		s.Data["json"] = data
		s.ServeJSON()
		s.StopRun()
	}
}

// 修改客户端
func (s *ClientController) Edit() {
	id := s.GetIntNoErr("id")
	if s.Ctx.Request.Method == "GET" {
		s.Data["menu"] = "client"
		if c, err := file.GetDb().GetClient(id); err != nil {
			s.error()
		} else {
			s.Data["c"] = c
			s.Data["BlackIpList"] = strings.Join(c.BlackIpList, "\r\n")
		}
		s.SetInfo("edit client")
		s.display()
	} else {
		if c, err := file.GetDb().GetClient(id); err != nil {
			s.error()
			s.AjaxErr("client ID not found")
			return
		} else {
			if s.GetSession("isAdmin").(bool) {
				if !file.GetDb().VerifyVkey(s.getEscapeString("vkey"), c.Id) {
					s.AjaxErr("Vkey duplicate, please reset")
					return
				}
				c.VerifyKey = s.getEscapeString("vkey")
				c.Flow.FlowLimit = int64(s.GetIntNoErr("flow_limit"))
				c.RateLimit = s.GetIntNoErr("rate_limit")
				c.MaxConn = s.GetIntNoErr("max_conn")
				c.MaxTunnelNum = s.GetIntNoErr("max_tunnel")
			}
			c.Remark = s.getEscapeString("remark")
			c.Cnf.U = s.getEscapeString("u")
			c.Cnf.P = s.getEscapeString("p")
			c.Cnf.Compress = common.GetBoolByStr(s.getEscapeString("compress"))
			c.Cnf.Crypt = s.GetBoolNoErr("crypt")
			b, err := beego.AppConfig.Bool("allow_user_change_username")
			if s.GetSession("isAdmin").(bool) || (err == nil && b) {
				c.WebUserName = s.getEscapeString("web_username")
			}
			c.WebPassword = s.getEscapeString("web_password")
			c.ConfigConnAllow = s.GetBoolNoErr("config_conn_allow")
			if c.Rate != nil {
				c.Rate.Stop()
			}
			if c.RateLimit > 0 {
				c.Rate = rate.NewRate(int64(c.RateLimit * 1024))
				c.Rate.Start()
			} else {
				c.Rate = rate.NewRate(int64(2 << 23))
				c.Rate.Start()
			}

			c.BlackIpList = RemoveRepeatedElement(strings.Split(s.getEscapeString("blackiplist"), "\r\n"))
			// No need to store to JSON file anymore as we're using MySQL

			// Restart all TCP tasks for this client
			tasks, _ := file.GetDb().GetTasksByClientId(id)
			for _, task := range tasks {
				if task.Mode == "tcp" {
					server.StopServer(task.Id)
					server.StartTask(task.Id)
				}
			}
		}
		s.AjaxOk("save success")
	}
}

func RemoveRepeatedElement(arr []string) (newArr []string) {
	newArr = make([]string, 0)
	for i := 0; i < len(arr); i++ {
		repeat := false
		for j := i + 1; j < len(arr); j++ {
			if arr[i] == arr[j] {
				repeat = true
				break
			}
		}
		if !repeat {
			newArr = append(newArr, arr[i])
		}
	}
	return
}

// 更改状态
func (s *ClientController) ChangeStatus() {
	id := s.GetIntNoErr("id")
	if client, err := file.GetDb().GetClient(id); err == nil {
		client.Status = s.GetBoolNoErr("status")
		if client.Status == false {
			server.DelClientConnect(client.Id)
		}
		s.AjaxOk("modified success")
	}
	s.AjaxErr("modified fail")
}

// 删除客户端
func (s *ClientController) Del() {
	id := s.GetIntNoErr("id")
	if err := file.GetDb().DelClient(id); err != nil {
		s.AjaxErr("delete error")
	}
	server.DelTunnelAndHostByClientId(id, false)
	server.DelClientConnect(id)
	s.AjaxOk("delete success")
}
