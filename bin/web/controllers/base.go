package controllers

import (
	"fmt"
	"html"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"ehang.io/nps/bridge"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/crypt"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/server"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
	"github.com/golang-jwt/jwt/v4"
)

type BaseController struct {
	beego.Controller
	controllerName string
	actionName     string
}

func (s *BaseController) TokenFail() {
	s.Ctx.Output.SetStatus(401)
	s.Data["json"] = map[string]interface{}{
		"code": 401,
		"msg":  "Invalid or expired token",
	}
	s.ServeJSON()
	s.StopRun()
}

// 初始化参数
func (s *BaseController) Prepare() {
	s.Data["web_base_url"] = beego.AppConfig.String("web_base_url")
	controllerName, actionName := s.GetControllerAndAction()
	s.controllerName = strings.ToLower(controllerName[0 : len(controllerName)-10])
	s.actionName = strings.ToLower(actionName)
	if "paymentcallback" == s.actionName {
		return
	}
	md5Key := s.getEscapeString("auth_key")

	// web api verify
	// support both token and session auth
	authToken := s.Ctx.Input.Header("Authorization")
	if authToken != "" && strings.HasPrefix(authToken, "Bearer ") {
		token := strings.TrimPrefix(authToken, "Bearer ")
		if isValidToken(token, s) {
			s.SetSession("isAdmin", true)
			s.Data["isAdmin"] = true
			// Set common configs for both token and session auth
			s.setCommonConfigs()
			return
		} else {
			s.TokenFail()
			return
		}
	} else if authToken == "" && s.GetSession("username") == nil {
		// For API requests, return 401
		if strings.HasPrefix(s.Ctx.Input.Header("Accept"), "application/json") {
			s.TokenFail()
			return
		}
		// For non-login page requests, redirect to login
		if s.controllerName != "login" || s.actionName != "index" {
			s.Redirect(beego.AppConfig.String("web_base_url")+"/login/index", 302)
			return
		}
		return
	}

	// check old auth method

	timestamp := s.GetIntNoErr("timestamp")
	configKey := beego.AppConfig.String("auth_key")
	if configKey == "" {
		configKey = crypt.GetRandomString(64)
	}
	timeNowUnix := time.Now().Unix()
	if !(md5Key != "" && (math.Abs(float64(timeNowUnix-int64(timestamp))) <= 20) && (crypt.Md5(configKey+strconv.Itoa(timestamp)) == md5Key)) {
		if s.GetSession("auth") != true {
			s.Redirect(beego.AppConfig.String("web_base_url")+"/login/index", 302)
		}
	} else {
		s.SetSession("isAdmin", true)
		s.Data["isAdmin"] = true
	}
	if s.GetSession("isAdmin") != nil && !s.GetSession("isAdmin").(bool) {
		s.Ctx.Input.SetData("client_id", s.GetSession("clientId").(int))
		s.Ctx.Input.SetParam("client_id", strconv.Itoa(s.GetSession("clientId").(int)))
		s.Data["isAdmin"] = false
		s.Data["username"] = s.GetSession("username")
		s.CheckUserAuth()
	} else {
		s.Data["isAdmin"] = true
	}

	if s.GetSessionIntNoErr("accountId", 0) == 0 && s.GetSession("username") != nil {
		username := s.GetSession("username").(string)
		account, err := file.GetDb().GetByUsername(username)
		if err != nil {

		} else {
			s.Data["accountId"] = account.Id
			s.SetSession("accountId", account.Id)
		}
	}

	s.Data["https_just_proxy"], _ = beego.AppConfig.Bool("https_just_proxy")
	s.Data["allow_user_login"], _ = beego.AppConfig.Bool("allow_user_login")
	s.Data["allow_flow_limit"], _ = beego.AppConfig.Bool("allow_flow_limit")
	s.Data["allow_rate_limit"], _ = beego.AppConfig.Bool("allow_rate_limit")
	s.Data["allow_connection_num_limit"], _ = beego.AppConfig.Bool("allow_connection_num_limit")
	s.Data["allow_multi_ip"], _ = beego.AppConfig.Bool("allow_multi_ip")
	s.Data["system_info_display"], _ = beego.AppConfig.Bool("system_info_display")
	s.Data["allow_tunnel_num_limit"], _ = beego.AppConfig.Bool("allow_tunnel_num_limit")
	s.Data["allow_local_proxy"], _ = beego.AppConfig.Bool("allow_local_proxy")
	s.Data["allow_user_change_username"], _ = beego.AppConfig.Bool("allow_user_change_username")
}

// 加载模板
// isValidToken validates JWT token
func isValidToken(token string, s *BaseController) bool {
	// Use auth_key from config as JWT secret
	secret := beego.AppConfig.String("auth_key")
	if secret == "" {
		secret = crypt.GetRandomString(64)
	}

	// Parse and validate the token
	parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secret), nil
	})

	if err != nil {
		logs.Error("Token validation failed:", err)
		return false
	}

	if claims, ok := parsedToken.Claims.(jwt.MapClaims); ok && parsedToken.Valid {
		// Check token expiration
		if exp, ok := claims["exp"].(float64); ok {
			if time.Now().Unix() > int64(exp) {
				logs.Error("Token expired")
				return false
			}
		}
		s.Data["username"] = claims["username"]
		s.Data["accountId"] = claims["accountId"]
		s.SetSession("username", claims["username"])
		s.SetSession("accountId", claims["accountId"])
		return true
	}

	return false
}

// setCommonConfigs sets common configuration values for both token and session auth
func (s *BaseController) setCommonConfigs() {
	s.Data["https_just_proxy"], _ = beego.AppConfig.Bool("https_just_proxy")
	s.Data["allow_user_login"], _ = beego.AppConfig.Bool("allow_user_login")
	s.Data["allow_flow_limit"], _ = beego.AppConfig.Bool("allow_flow_limit")
	s.Data["allow_rate_limit"], _ = beego.AppConfig.Bool("allow_rate_limit")
	s.Data["allow_connection_num_limit"], _ = beego.AppConfig.Bool("allow_connection_num_limit")
	s.Data["allow_multi_ip"], _ = beego.AppConfig.Bool("allow_multi_ip")
	s.Data["system_info_display"], _ = beego.AppConfig.Bool("system_info_display")
	s.Data["allow_tunnel_num_limit"], _ = beego.AppConfig.Bool("allow_tunnel_num_limit")
	s.Data["allow_local_proxy"], _ = beego.AppConfig.Bool("allow_local_proxy")
	s.Data["allow_user_change_username"], _ = beego.AppConfig.Bool("allow_user_change_username")
}

func (s *BaseController) display(tpl ...string) {
	s.Data["web_base_url"] = beego.AppConfig.String("web_base_url")
	var tplname string
	if s.Data["menu"] == nil {
		s.Data["menu"] = s.actionName
	}
	if len(tpl) > 0 {
		tplname = strings.Join([]string{tpl[0], "html"}, ".")
	} else {
		tplname = s.controllerName + "/" + s.actionName + ".html"
	}
	ip := s.Ctx.Request.Host
	s.Data["ip"] = common.GetIpByAddr(ip)
	s.Data["bridgeType"] = beego.AppConfig.String("bridge_type")
	if common.IsWindows() {
		s.Data["win"] = ".exe"
	}

	s.Data["p"] = strconv.Itoa(server.Bridge.TunnelPort)

	if bridge.ServerTlsEnable {
		tlsPort := strconv.Itoa(beego.AppConfig.DefaultInt("tls_bridge_port", 8025))
		s.Data["tls_p"] = tlsPort
		s.Data["p1"] = strconv.Itoa(server.Bridge.TunnelPort) + " / " + tlsPort
	} else {
		s.Data["p1"] = strconv.Itoa(server.Bridge.TunnelPort)
	}

	s.Data["proxyPort"] = beego.AppConfig.String("hostPort")
	s.Layout = "public/layout.html"
	s.TplName = tplname
}

// 错误
func (s *BaseController) error() {
	s.Data["web_base_url"] = beego.AppConfig.String("web_base_url")
	s.Layout = "public/layout.html"
	s.TplName = "public/error.html"
}

// getEscapeString
func (s *BaseController) getEscapeString(key string) string {
	return html.EscapeString(s.GetString(key))
}

// 去掉没有err返回值的int
func (s *BaseController) GetIntNoErr(key string, def ...int) int {
	strv := s.Ctx.Input.Query(key)
	if len(strv) == 0 && len(def) > 0 {
		return def[0]
	}
	val, _ := strconv.Atoi(strv)
	return val
}

// 去掉没有err返回值的int
func (s *BaseController) GetSessionIntNoErr(key string, def ...int) int {
	sessionValue := s.GetSession(key)
	fmt.Println("GetSessionIntNoErr err:", sessionValue)
	var myIntValue int
	var err error
	fmt.Println("GetSessionIntNoErr unexpected type:", reflect.TypeOf(sessionValue))
	// 检查 session 中是否存在该值
	if sessionValue != nil {
		// 如果存储的是字符串形式，则需要转换
		switch v := sessionValue.(type) {
		case int:
			myIntValue = v
		case string:
			myIntValue, err = strconv.Atoi(v)
		case float64: // 处理可能的浮点数类型
			myIntValue = int(v)
		default:
			fmt.Println("GetSessionIntNoErr unexpected type:", sessionValue)
			myIntValue = def[0]
		}
	} else {
		myIntValue = def[0]
		fmt.Println("GetSessionIntNoErr err2:", err)
	}
	return myIntValue
}

// 获取去掉错误的bool值
func (s *BaseController) GetBoolNoErr(key string, def ...bool) bool {
	strv := s.Ctx.Input.Query(key)
	if len(strv) == 0 && len(def) > 0 {
		return def[0]
	}
	val, _ := strconv.ParseBool(strv)
	return val
}

// ajax正确返回
func (s *BaseController) AjaxOk(str string) {
	s.Data["json"] = ajax(str, 200)
	s.ServeJSON()
	s.StopRun()
}

// ajax正确返回
func (s *BaseController) AjaxOkWithId(str string, id int) {
	s.Data["json"] = ajaxWithId(str, 200, id)
	s.ServeJSON()
	s.StopRun()
}

// ajax错误返回
func (s *BaseController) AjaxErr(str string) {
	s.Data["json"] = ajax(str, 400)
	s.ServeJSON()
	s.StopRun()
}

// 组装ajax
func ajax(str string, code int) map[string]interface{} {
	json := make(map[string]interface{})
	json["code"] = code
	json["msg"] = str
	json["data"] = nil
	return json
}

// 组装ajax
func ajaxWithId(str string, code int, id int) map[string]interface{} {
	json := make(map[string]interface{})
	json["code"] = code
	json["msg"] = str
	json["data"] = map[string]interface{}{
		"id": id,
	}
	return json
}

// ajax table返回
func (s *BaseController) AjaxTable(list interface{}, cnt int, recordsTotal int, kwargs map[string]interface{}) {
	json := make(map[string]interface{})
	json["code"] = 200
	json["msg"] = "success"
	json["data"] = map[string]interface{}{
		"rows":  list,
		"total": recordsTotal,
	}
	if kwargs != nil {
		for k, v := range kwargs {
			if v != nil {
				json["data"].(map[string]interface{})[k] = v
			}
		}
	}
	s.Data["json"] = json
	s.ServeJSON()
	s.StopRun()
}

// ajax table参数
func (s *BaseController) GetAjaxParams() (start, limit int) {
	return s.GetIntNoErr("offset"), s.GetIntNoErr("limit")
}

func (s *BaseController) SetInfo(name string) {
	s.Data["name"] = name
}

func (s *BaseController) SetType(name string) {
	s.Data["type"] = name
}

func (s *BaseController) CheckUserAuth() {
	if s.controllerName == "client" {
		if s.actionName == "add" {
			s.StopRun()
			return
		}
		if id := s.GetIntNoErr("id"); id != 0 {
			if id != s.GetSession("clientId").(int) {
				s.StopRun()
				return
			}
		}
	}
	if s.controllerName == "index" {
		if id := s.GetIntNoErr("id"); id != 0 {
			belong := false
			if strings.Contains(s.actionName, "h") {
				if host, err := file.GetDb().GetHostById(id); err == nil {
					if host.Client.Id == s.GetSession("clientId").(int) {
						belong = true
					}
				}
			} else {
				if task, err := file.GetDb().GetTask(id); err == nil {
					if task.Client.Id == s.GetSession("clientId").(int) {
						belong = true
					}
				}
			}
			if !belong {
				s.StopRun()
			}
		}
	}
}
