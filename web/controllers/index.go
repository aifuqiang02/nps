package controllers

import (
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"ehang.io/nps/lib/file"
	"ehang.io/nps/server"
	"ehang.io/nps/server/tool"

	"github.com/astaxie/beego"
)

type IndexController struct {
	BaseController
}

func (s *IndexController) Index() {
	s.Data["web_base_url"] = beego.AppConfig.String("web_base_url")
	s.Data["data"] = server.GetDashboardData()
	s.SetInfo("dashboard")
	s.display("index/index")
}
func (s *IndexController) Help() {
	s.SetInfo("about")
	s.display("index/help")
}

func (s *IndexController) Tcp() {
	s.SetInfo("tcp")
	s.SetType("tcp")
	s.display("index/list")
}

func (s *IndexController) Udp() {
	s.SetInfo("udp")
	s.SetType("udp")
	s.display("index/list")
}

func (s *IndexController) Socks5() {
	s.SetInfo("socks5")
	s.SetType("socks5")
	s.display("index/list")
}

func (s *IndexController) Http() {
	s.SetInfo("http proxy")
	s.SetType("httpProxy")
	s.display("index/list")
}
func (s *IndexController) File() {
	s.SetInfo("file server")
	s.SetType("file")
	s.display("index/list")
}

func (s *IndexController) Secret() {
	s.SetInfo("secret")
	s.SetType("secret")
	s.display("index/list")
}
func (s *IndexController) P2p() {
	s.SetInfo("p2p")
	s.SetType("p2p")
	s.display("index/list")
}

func (s *IndexController) Host() {
	s.SetInfo("host")
	s.SetType("hostServer")
	s.display("index/list")
}

func (s *IndexController) All() {
	s.Data["menu"] = "client"
	clientId := s.getEscapeString("client_id")
	s.Data["client_id"] = clientId
	s.SetInfo("client id:" + clientId)
	s.display("index/list")
}

func (s *IndexController) GetTunnel() {
	start, length := s.GetAjaxParams()
	taskType := s.getEscapeString("type")
	clientId := s.GetIntNoErr("client_id")
	list, cnt := server.GetTunnel(start, length, taskType, clientId, s.getEscapeString("search"), s.getEscapeString("sort"), s.getEscapeString("order"))
	s.AjaxTable(list, cnt, cnt, nil)
}

func (s *IndexController) GetTunnelV2() {
	start, length := s.GetAjaxParams()
	taskType := s.getEscapeString("type")
	clientId := s.GetIntNoErr("client_id")
	fmt.Println("GetTunnelV2 clientId:", clientId)
	accountId := s.GetSessionIntNoErr("accountId", 0)
	fmt.Println("GetTunnelV2 accountId:", accountId)
	list, cnt := server.GetTunnelV2(start, length, taskType, accountId, clientId, s.getEscapeString("search"), s.getEscapeString("sort"), s.getEscapeString("order"))
	s.AjaxTable(list, cnt, cnt, nil)
}

func (s *IndexController) Add() {
	if s.Ctx.Request.Method == "GET" {
		s.Data["type"] = s.getEscapeString("type")
		s.Data["client_id"] = s.getEscapeString("client_id")
		s.SetInfo("add tunnel")
		s.display()
	} else {
		id := int(file.GetDb().GetNewTaskId())

		t := &file.Tunnel{
			Port:      s.GetIntNoErr("port"),
			ServerIp:  s.getEscapeString("server_ip"),
			AccountId: s.GetSessionIntNoErr("accountId"),
			Mode:      s.getEscapeString("mode"),
			Target:    &file.Target{TargetStr: s.getEscapeString("target"), LocalProxy: s.GetBoolNoErr("local_proxy")},
			Id:        id,
			Status:    true,
			Remark:    s.getEscapeString("remark"),
			Password:  s.getEscapeString("password"),
			LocalPath: s.getEscapeString("local_path"),
			StripPre:  s.getEscapeString("strip_pre"),
			Flow:      &file.Flow{},
		}

		if client, err := file.GetDb().GetClient(s.GetIntNoErr("client_id")); err != nil {
			s.AjaxErr("modified error,the client is not exist")
			return
		} else {
			t.Client = client
			t.ClientId = client.Id
		}

		if t.Port <= 0 {
			t.Port = tool.GenerateServerPort(t.Mode)
		}
		if t.Mode == "http" || t.Mode == "https" {
			t.ExternalServiceDomain = t.Mode + "://" + strconv.Itoa(t.Port) + "." + beego.AppConfig.String("external_service_domain")
		} else {
			t.ExternalServiceDomain = beego.AppConfig.String("external_service_ip") + ":" + strconv.Itoa(t.Port)
		}

		if !tool.TestServerPort(t.Port, t.Mode) {
			s.AjaxErr("The port cannot be opened because it may has been occupied or is no longer allowed.")
		}
		var err error
		if t.Client, err = file.GetDb().GetClient(s.GetIntNoErr("client_id")); err != nil {
			s.AjaxErr(err.Error())
		}
		if t.Client.MaxTunnelNum != 0 && t.Client.GetTunnelNum() >= t.Client.MaxTunnelNum {
			s.AjaxErr("The number of tunnels exceeds the limit")
		}
		if err := file.GetDb().NewTask(t); err != nil {
			s.AjaxErr(err.Error())
		}
		if err := server.AddTask(t); err != nil {
			s.AjaxErr(err.Error())
		} else {
			s.AjaxOkWithId("add success", id)
		}
	}
}
func (s *IndexController) GetOneTunnel() {
	id := s.GetIntNoErr("id")
	data := make(map[string]interface{})
	if t, err := file.GetDb().GetTask(id); err != nil {
		data["code"] = 0
	} else {
		data["code"] = 1
		data["data"] = t
	}
	s.Data["json"] = data
	s.ServeJSON()
}
func (s *IndexController) Edit() {
	id := s.GetIntNoErr("id")
	if s.Ctx.Request.Method == "GET" {
		if t, err := file.GetDb().GetTask(id); err != nil {
			s.error()
		} else {
			s.Data["t"] = t
		}
		s.SetInfo("edit tunnel")
		s.display()
	} else {
		if t, err := file.GetDb().GetTask(id); err != nil {
			s.error()
		} else {
			if s.GetIntNoErr("port") != t.Port || t.Port == 0 {
				t.Port = s.GetIntNoErr("port")

				if t.Port <= 0 {
					t.Port = tool.GenerateServerPort(t.Mode)
				}

				if !tool.TestServerPort(s.GetIntNoErr("port"), t.Mode) {
					s.AjaxErr("The port cannot be opened because it may has been occupied or is no longer allowed.")
					return
				}
			}
			if t.Mode == "http" || t.Mode == "https" {
				t.ExternalServiceDomain = t.Mode + "://" + strconv.Itoa(t.Port) + "." + beego.AppConfig.String("external_service_domain")
			} else {
				t.ExternalServiceDomain = beego.AppConfig.String("external_service_ip") + ":" + strconv.Itoa(t.Port)
			}
			t.ServerIp = s.getEscapeString("server_ip")
			t.Mode = s.getEscapeString("mode")
			t.Target = &file.Target{TargetStr: s.getEscapeString("target")}
			t.Password = s.getEscapeString("password")
			t.Id = id
			t.LocalPath = s.getEscapeString("local_path")
			t.StripPre = s.getEscapeString("strip_pre")
			t.Remark = s.getEscapeString("remark")
			t.Target.LocalProxy = s.GetBoolNoErr("local_proxy")
			file.GetDb().UpdateTask(t)
			server.StopServer(t.Id)
			server.StartTask(t.Id)
		}
		s.AjaxOk("modified success")
	}
}

func (s *IndexController) Stop() {
	id := s.GetIntNoErr("id")
	if err := server.StopServer(id); err != nil {
		s.AjaxErr("stop error")
	}
	s.AjaxOk("stop success")
}

func (s *IndexController) Del() {
	id := s.GetIntNoErr("id")
	if err := server.DelTask(id); err != nil {
		s.AjaxErr("delete error")
	}
	s.AjaxOk("delete success")
}

func (s *IndexController) Start() {
	id := s.GetIntNoErr("id")
	if err := server.StartTask(id); err != nil {
		s.AjaxErr("start error")
	}
	s.AjaxOk("start success")
}

func (s *IndexController) HostList() {
	if s.Ctx.Request.Method == "GET" {
		s.Data["client_id"] = s.getEscapeString("client_id")
		s.Data["menu"] = "host"
		s.SetInfo("host list")
		s.display("index/hlist")
	} else {
		start, length := s.GetAjaxParams()
		clientId := s.GetIntNoErr("client_id")
		list, cnt, err := file.GetDb().GetHost(start, length, clientId, s.getEscapeString("search"))
		if err != nil {
			s.AjaxErr(err.Error())
			return
		}
		s.AjaxTable(list, cnt, cnt, nil)
	}
}

func (s *IndexController) GetHost() {
	if s.Ctx.Request.Method == "POST" {
		data := make(map[string]interface{})
		if h, err := file.GetDb().GetHostById(s.GetIntNoErr("id")); err != nil {
			data["code"] = 0
		} else {
			data["data"] = h
			data["code"] = 1
		}
		s.Data["json"] = data
		s.ServeJSON()
	}
}

func (s *IndexController) DelHost() {
	id := s.GetIntNoErr("id")
	if err := file.GetDb().DelHost(id); err != nil {
		s.AjaxErr("delete error")
	}
	s.AjaxOk("delete success")
}

func (s *IndexController) AddHost() {
	if s.Ctx.Request.Method == "GET" {
		s.Data["client_id"] = s.getEscapeString("client_id")
		s.Data["menu"] = "host"
		s.SetInfo("add host")
		s.display("index/hadd")
	} else {
		id := int(file.GetDb().GetNewHostId())
		h := &file.Host{
			Id:           id,
			Host:         s.getEscapeString("host"),
			Target:       &file.Target{TargetStr: s.getEscapeString("target"), LocalProxy: s.GetBoolNoErr("local_proxy")},
			HeaderChange: s.getEscapeString("header"),
			HostChange:   s.getEscapeString("hostchange"),
			Remark:       s.getEscapeString("remark"),
			Location:     s.getEscapeString("location"),
			Flow:         &file.Flow{},
			Scheme:       s.getEscapeString("scheme"),
			KeyFilePath:  s.getEscapeString("key_file_path"),
			CertFilePath: s.getEscapeString("cert_file_path"),
			AutoHttps:    s.GetBoolNoErr("AutoHttps"),
		}
		var err error
		if h.Client, err = file.GetDb().GetClient(s.GetIntNoErr("client_id")); err != nil {
			s.AjaxErr("add error the client can not be found")
		}
		if h.Client.MaxTunnelNum != 0 && h.Client.GetTunnelNum() >= h.Client.MaxTunnelNum {
			s.AjaxErr("The number of tunnels exceeds the limit")
		}

		if err := file.GetDb().NewHost(h); err != nil {
			s.AjaxErr("add fail" + err.Error())
		}
		s.AjaxOkWithId("add success", id)
	}
}

func (s *IndexController) EditHost() {
	id := s.GetIntNoErr("id")
	if s.Ctx.Request.Method == "GET" {
		s.Data["menu"] = "host"
		if h, err := file.GetDb().GetHostById(id); err != nil {
			s.error()
		} else {
			s.Data["h"] = h
		}
		s.SetInfo("edit")
		s.display("index/hedit")
	} else {
		if h, err := file.GetDb().GetHostById(id); err != nil {
			s.error()
		} else {
			if h.Host != s.getEscapeString("host") {
				tmpHost := new(file.Host)
				tmpHost.Host = s.getEscapeString("host")
				tmpHost.Location = s.getEscapeString("location")
				tmpHost.Scheme = s.getEscapeString("scheme")
				if file.GetDb().IsHostExist(tmpHost) {
					s.AjaxErr("host has exist")
					return
				}
			}
			if client, err := file.GetDb().GetClient(s.GetIntNoErr("client_id")); err != nil {
				s.AjaxErr("modified error,the client is not exist")
			} else {
				h.Client = client
			}
			h.Host = s.getEscapeString("host")
			h.Target = &file.Target{TargetStr: s.getEscapeString("target")}
			h.HeaderChange = s.getEscapeString("header")
			h.HostChange = s.getEscapeString("hostchange")
			h.Remark = s.getEscapeString("remark")
			h.Location = s.getEscapeString("location")
			h.Scheme = s.getEscapeString("scheme")
			h.KeyFilePath = s.getEscapeString("key_file_path")
			h.CertFilePath = s.getEscapeString("cert_file_path")
			h.Target.LocalProxy = s.GetBoolNoErr("local_proxy")
			h.AutoHttps = s.GetBoolNoErr("AutoHttps")
			// No need to store to JSON file anymore as we're using MySQL
		}
		s.AjaxOk("modified success")
	}
}

func (s *IndexController) GetPricePlan() {
	data := map[string]interface{}{
		"code": 200,
		"msg":  "success",
		"data": map[string]float64{
			"pricePerGB":    1.0, // 每GB流量价格(元)
			"pricePerMonth": 5.0, // 每月基础费用(元)
		},
	}
	s.Data["json"] = data
	s.ServeJSON()
}

func (s *IndexController) CreatePaymentOrder() {
	paymentType := s.getEscapeString("paymentType")
	months := s.GetIntNoErr("months")
	flow := s.GetIntNoErr("flow")
	accountId := s.GetSessionIntNoErr("accountId", 0)

	// 计算订单金额
	var orderAmount float64
	if paymentType == "traffic" {
		orderAmount = float64(flow) * 1.0 // 每GB流量价格(元)
	} else {
		orderAmount = float64(months) * 5.0 // 每月基础费用(元)
	}

	// 创建订单对象
	order := &file.Order{
		AppId:                 "b8e47ca842ac4ce18d4e17b5bee46f91",
		OrderAmount:           orderAmount,
		Flow:                  float64(flow),
		Months:                months,
		OrderStatus:           "pending",
		PaymentType:           paymentType,
		ExternalTransactionId: fmt.Sprintf("PAY%s%d", time.Now().Format("20060102150405"), rand.Intn(1000)),
		AccountId:             strconv.Itoa(accountId),
	}

	// 保存订单到数据库
	if err := file.GetDb().CreateOrder(order); err != nil {
		s.AjaxErr("创建订单失败: " + err.Error())
		return
	}

	// 返回订单信息
	data := make(map[string]interface{})
	data["code"] = 200
	data["msg"] = "订单创建成功"
	data["data"] = map[string]interface{}{
		"orderAmount":           order.OrderAmount,
		"appId":                 order.AppId,
		"externalTransactionId": order.ExternalTransactionId,
	}

	s.Data["json"] = data
	s.ServeJSON()
}
