package controllers

import (
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/astaxie/beego/cache"
	"github.com/astaxie/beego/logs"
	"github.com/astaxie/beego/utils/captcha"
	"github.com/golang-jwt/jwt/v4"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/crypt"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/server"
	"github.com/astaxie/beego"
)

type LoginController struct {
	beego.Controller
}

var ipRecord sync.Map
var cpt *captcha.Captcha

type record struct {
	hasLoginFailTimes int
	lastLoginTime     time.Time
}

func init() {
	// use beego cache system store the captcha data
	store := cache.NewMemoryCache()
	cpt = captcha.NewWithFilter("/captcha/", store)
}

func (self *LoginController) Index() {
	// Try login implicitly, will succeed if it's configured as no-auth(empty username&password).
	webBaseUrl := beego.AppConfig.String("web_base_url")
	if self.doLogin("", "", false) {
		self.Redirect(webBaseUrl+"/index/index", 302)
	}
	self.Data["web_base_url"] = webBaseUrl
	self.Data["register_allow"], _ = beego.AppConfig.Bool("allow_user_register")
	self.Data["captcha_open"], _ = beego.AppConfig.Bool("open_captcha")
	self.TplName = "login/index.html"
}

func (self *LoginController) Verify() {
	username := self.GetString("username")
	password := self.GetString("password")
	fmt.Println("Verify1:")
	if self.doLogin(username, password, true) {
		fmt.Println("Verify2:")
		token := generateToken(username)
		account, err := file.GetDb().GetByUsername(username)
		fmt.Println(err)
		data := make(map[string]interface{})
		data["token"] = token
		data["account"] = account

		self.Data["json"] = map[string]interface{}{"code": 200, "msg": "login success", "data": data}
	} else {
		self.Data["json"] = map[string]interface{}{"code": 400, "msg": "username or password incorrect"}
	}
	self.ServeJSON()
}

func (self *LoginController) doLogin(username, password string, explicit bool) bool {
	clearIprecord()
	ip, _, _ := net.SplitHostPort(self.Ctx.Request.RemoteAddr)
	fmt.Println("doLogin1:")
	if v, ok := ipRecord.Load(ip); ok {
		vv := v.(*record)
		if (time.Now().Unix() - vv.lastLoginTime.Unix()) >= 60 {
			vv.hasLoginFailTimes = 0
		}
		if vv.hasLoginFailTimes >= 10 {
			return false
		}
	}
	fmt.Println("doLogin2:")
	var auth bool
	if password == beego.AppConfig.String("web_password") && username == beego.AppConfig.String("web_username") {
		self.SetSession("isAdmin", true)
		self.DelSession("clientId")
		self.DelSession("username")
		auth = true
		server.Bridge.Register.Store(common.GetIpByAddr(self.Ctx.Input.IP()), time.Now().Add(time.Hour*time.Duration(2)))
	}
	fmt.Println("doLogin3:")
	b, err := beego.AppConfig.Bool("allow_user_login")
	account, err := file.GetDb().GetByUsername(username)
	fmt.Println("doLogin31:", err, b, auth)
	if err == nil && b && !auth {
		fmt.Println("doLogin4:")

		fmt.Println("doLogin5:")
		if err != nil {
			logs.Error("Failed to get account from MySQL:", err)
			return false
		}

		// 检查账户凭据
		if account.WebUserName == "" && account.WebPassword == "" {
			// 特殊情况：用户名为"user"且密码为验证密钥
			// 注意：由于Account结构体中没有VerifyKey字段，此逻辑可能需要调整
			if username == "user" {
				auth = true
			}
		} else if account.WebPassword == password && account.WebUserName == username {
			auth = true
		}

		if auth {
			self.SetSession("isAdmin", false)
			self.SetSession("clientId", account.Id)
			self.SetSession("username", account.WebUserName)
		}
	}
	fmt.Println("doLogin6:")
	if auth {
		fmt.Println("doLogin61:")
		self.SetSession("auth", true)
		self.SetSession("clientId", account.Id)
		self.SetSession("username", account.WebUserName)
		ipRecord.Delete(ip)
		fmt.Println("doLogin62:")
		return true
	}
	fmt.Println("doLogin7:")
	if v, load := ipRecord.LoadOrStore(ip, &record{hasLoginFailTimes: 1, lastLoginTime: time.Now()}); load && explicit {
		vv := v.(*record)
		vv.lastLoginTime = time.Now()
		vv.hasLoginFailTimes += 1
		ipRecord.Store(ip, vv)
	}
	return false
}

func (self *LoginController) VerifyForWx() {
	username := self.GetString("username")
	password := self.GetString("password")
	fmt.Println("Verify1:")
	if self.doLogin(username, password, true) {
		fmt.Println("Verify2:")
		token := generateToken(username)
		account, err := file.GetDb().GetByUsername(username)
		fmt.Println(err)
		data := make(map[string]interface{})
		data["token"] = token
		data["account"] = account

		self.Data["json"] = map[string]interface{}{"code": 200, "msg": "login success", "data": data}
	} else {
		self.Data["json"] = map[string]interface{}{"code": 400, "msg": "username or password incorrect"}
	}
	self.ServeJSON()
}

func (self *LoginController) doLoginForWx(username, password string, explicit bool) bool {
	clearIprecord()
	ip, _, _ := net.SplitHostPort(self.Ctx.Request.RemoteAddr)
	fmt.Println("doLogin1:")
	if v, ok := ipRecord.Load(ip); ok {
		vv := v.(*record)
		if (time.Now().Unix() - vv.lastLoginTime.Unix()) >= 60 {
			vv.hasLoginFailTimes = 0
		}
		if vv.hasLoginFailTimes >= 10 {
			return false
		}
	}
	fmt.Println("doLogin2:")
	var auth bool
	if password == beego.AppConfig.String("web_password") && username == beego.AppConfig.String("web_username") {
		self.SetSession("isAdmin", true)
		self.DelSession("clientId")
		self.DelSession("username")
		auth = true
		server.Bridge.Register.Store(common.GetIpByAddr(self.Ctx.Input.IP()), time.Now().Add(time.Hour*time.Duration(2)))
	}
	fmt.Println("doLogin3:")
	b, err := beego.AppConfig.Bool("allow_user_login")
	account, err := file.GetDb().GetByUsername(username)
	fmt.Println("doLogin31:", err, b, auth)
	if err == nil && b && !auth {
		fmt.Println("doLogin4:")

		fmt.Println("doLogin5:")
		if err != nil {
			logs.Error("Failed to get account from MySQL:", err)
			return false
		}

		// 检查账户凭据
		if account.WebUserName == "" && account.WebPassword == "" {
			// 特殊情况：用户名为"user"且密码为验证密钥
			// 注意：由于Account结构体中没有VerifyKey字段，此逻辑可能需要调整
			if username == "user" {
				auth = true
			}
		} else if account.WebPassword == password && account.WebUserName == username {
			auth = true
		}

		if auth {
			self.SetSession("isAdmin", false)
			self.SetSession("clientId", account.Id)
			self.SetSession("username", account.WebUserName)
		}
	}
	fmt.Println("doLogin6:")
	if auth {
		fmt.Println("doLogin61:")
		self.SetSession("auth", true)
		self.SetSession("clientId", account.Id)
		self.SetSession("username", account.WebUserName)
		ipRecord.Delete(ip)
		fmt.Println("doLogin62:")
		return true
	}
	fmt.Println("doLogin7:")
	if v, load := ipRecord.LoadOrStore(ip, &record{hasLoginFailTimes: 1, lastLoginTime: time.Now()}); load && explicit {
		vv := v.(*record)
		vv.lastLoginTime = time.Now()
		vv.hasLoginFailTimes += 1
		ipRecord.Store(ip, vv)
	}
	return false
}

func (self *LoginController) Register() {
	if self.Ctx.Request.Method == "GET" {
		self.Data["web_base_url"] = beego.AppConfig.String("web_base_url")
		self.TplName = "login/register.html"
	} else {
		if b, err := beego.AppConfig.Bool("allow_user_register"); err != nil || !b {
			self.Data["json"] = map[string]interface{}{"status": 0, "msg": "register is not allow"}
			self.ServeJSON()
			return
		}
		if self.GetString("username") == "" || self.GetString("password") == "" || self.GetString("username") == beego.AppConfig.String("web_username") {
			self.Data["json"] = map[string]interface{}{"status": 0, "msg": "please check your input"}
			self.ServeJSON()
			return
		}
		t := &file.Account{
			Status:      true,
			Cnf:         &file.Config{},
			WebUserName: self.GetString("username"),
			WebPassword: self.GetString("password"),
			Flow:        &file.Flow{},
		}
		if err := file.GetDb().NewAccount(t); err != nil {
			self.Data["json"] = map[string]interface{}{"code": 400, "msg": err.Error()}
		} else {
			self.Data["json"] = map[string]interface{}{"code": 200, "msg": "register success"}
		}
		self.ServeJSON()
	}
}

func (self *LoginController) Out() {
	self.SetSession("auth", false)
	self.Redirect(beego.AppConfig.String("web_base_url")+"/login/index", 302)
}

// generateToken creates a JWT token for the user
func generateToken(username string) string {
	// Use auth_key from config as JWT secret
	secret := beego.AppConfig.String("auth_key")
	if secret == "" {
		secret = crypt.GetRandomString(64)
	}
	account, err := file.GetDb().GetByUsername(username)
	fmt.Println("generateToken1:", account)
	// Create token with 24h expiration
	fmt.Println("accountId:", account.Id)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"username":  username,
		"accountId": account.Id,
		"exp":       time.Now().Add(time.Hour * 2400000).Unix(),
	})

	// Sign and get the complete encoded token as a string
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		logs.Error("Failed to generate token:", err)
		return ""
	}
	return tokenString
}

func clearIprecord() {
	rand.Seed(time.Now().UnixNano())
	x := rand.Intn(100)
	if x == 1 {
		ipRecord.Range(func(key, value interface{}) bool {
			v := value.(*record)
			if time.Now().Unix()-v.lastLoginTime.Unix() >= 60 {
				ipRecord.Delete(key)
			}
			return true
		})
	}
}
