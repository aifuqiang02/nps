package controllers

import (
	"encoding/json"
	"fmt"
	"strconv"

	"ehang.io/nps/lib/file"
	"ehang.io/nps/server"

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

func (s *IndexController) PaymentCallback() {
	// 记录请求体内容
	body := s.Ctx.Input.RequestBody
	fmt.Printf("PaymentCallback request body: %s\n", string(body))

	// 解析JSON参数
	type CallbackRequest struct {
		ExternalTransactionId string `json:"externalTransactionId"`
		Secret                string `json:"secret"`
	}
	var req CallbackRequest

	// 使用json.NewDecoder解析
	decoder := json.NewDecoder(s.Ctx.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		s.AjaxErr(fmt.Sprintf("参数解析失败: %v, body: %s", err, string(body)))
		return
	}

	// 验证secret
	if req.Secret != "b8e47ca842ac4ce18d4e17b5bee46f911111111" {
		s.AjaxErr("无效的secret")
		return
	}

	// 查询订单
	order, err := file.GetDb().GetOrderByExternalId(req.ExternalTransactionId)
	if err != nil {
		s.AjaxErr("订单不存在")
		return
	}
	if order.OrderStatus == "paid" {
		s.AjaxErr("订单已完成")
		return
	}

	// 更新订单状态
	order.OrderStatus = "paid"
	if err := file.GetDb().UpdateOrder(order); err != nil {
		s.AjaxErr("订单状态更新失败")
		return
	}

	// 执行账号充值逻辑
	accountId, _ := strconv.Atoi(order.AccountId)
	if order.PaymentType == "traffic" {
		// 流量充值逻辑
		if err := file.GetDb().AddTraffic(accountId, order.Flow); err != nil {
			s.AjaxErr("流量充值失败")
			return
		}
	} else {
		// 月费充值逻辑
		if err := file.GetDb().AddMonths(accountId, order.Months); err != nil {
			s.AjaxErr("月费充值失败")
			return
		}
	}

	s.AjaxOk("处理成功")
}
