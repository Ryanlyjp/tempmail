package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"tempmail/config"
	"tempmail/handler"
	"tempmail/middleware"
	"tempmail/store"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()

	// ==================== 连接数据库 ====================
	ctx := context.Background()
	db, err := store.New(ctx, cfg.DBDSN)
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}
	defer db.Close()
	log.Println("✓ Database connected")

	// ==================== 连接 Redis ====================
	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.RedisAddr,
		Password:     cfg.RedisPassword,
		DB:           0,
		PoolSize:     0, // 0 = 不限（自动按 CPU 核心数 * 10）
		MinIdleConns: 20,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to connect redis: %v", err)
	}
	defer rdb.Close()
	log.Println("✓ Redis connected")

	// ==================== Gin 路由 ====================
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// CORS：允许前端跨域访问
	r.Use(cors.New(cors.Config{
		AllowOrigins:  []string{"*"},
		AllowMethods:  []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:  []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders: []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"},
		MaxAge:        12 * time.Hour,
	}))

	// 健康检查（无需认证）
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "time": time.Now().Unix()})
	})

	// 初始化 handlers
	accountH := handler.NewAccountHandler(db)
	domainH := handler.NewDomainHandler(db, cfg.SMTPServerIP, cfg.SMTPHostname)
	hostnameH := handler.NewHostnameHandler(db)
	mailboxH := handler.NewMailboxHandler(db)
	emailH := handler.NewEmailHandler(db)
	settingH := handler.NewSettingHandler(db)
	registerH := handler.NewRegisterHandler(db)
	statsH := handler.NewStatsHandler(db)

	// 公开路由（无需认证）
	public := r.Group("/public")
	{
		public.GET("/settings", settingH.GetPublic)
		public.POST("/register", registerH.Register)
		public.GET("/stats", statsH.Get)
	}

	// API 路由组（需要认证 + 速率限制）
	api := r.Group("/api")
	api.Use(middleware.Auth(db))
	api.Use(middleware.RateLimit(rdb, cfg.RateLimit, cfg.RateWindow))
	{
		// 当前用户
		api.GET("/me", accountH.Me)

		// 域名池（所有用户可查看）
		api.GET("/domains", domainH.List)
		api.GET("/hostnames", hostnameH.List)
		api.GET("/domains/:id/status", domainH.GetStatus) // 任意用户可轮询域名状态
		api.GET("/stats", statsH.Get)
		// 任意已登录用户可提交域名进行 MX 自动验证
		api.POST("/domains/submit", domainH.Submit)

		// 邮箱管理
		api.POST("/mailboxes", mailboxH.Create)
		api.GET("/mailboxes", mailboxH.List)
		api.DELETE("/mailboxes/:id", mailboxH.Delete)
		api.PUT("/mailboxes/:id/favorite", mailboxH.Favorite)
		api.PUT("/mailboxes/:id/forward", mailboxH.Forward)

		// 邮件管理
		api.GET("/mailboxes/:id/emails", emailH.List)
		api.GET("/mailboxes/:id/emails/:email_id", emailH.Get)
		api.POST("/mailboxes/:id/emails/:email_id/forward/tg", emailH.ForwardTelegram)
		api.GET("/mailboxes/:id/emails/:email_id/attachments/:attachment_id", emailH.DownloadAttachment)
		api.GET("/mailboxes/:id/otp/latest", emailH.LatestOTP)
		api.DELETE("/mailboxes/:id/emails/:email_id", emailH.Delete)
		// 管理员路由
		admin := api.Group("/admin")
		admin.Use(middleware.AdminOnly())
		{
			admin.POST("/accounts", accountH.Create)
			admin.GET("/accounts", accountH.List)
			admin.DELETE("/accounts/:id", accountH.Delete)

			admin.POST("/domains", domainH.Add)
			admin.DELETE("/domains/:id", domainH.Delete)
			admin.DELETE("/domains/:id/cf", domainH.CFDelete)
			admin.PUT("/domains/:id/toggle", domainH.Toggle)
			admin.PUT("/domains/:id/hostname", domainH.UpdateHostname)
			admin.PUT("/domains/:id/subdomain", domainH.UpdateSubdomain)
			admin.PUT("/domains/batch/toggle", domainH.BatchToggle)
			admin.PUT("/domains/batch/delete", domainH.BatchDelete)
			admin.PUT("/domains/batch/subdomain", domainH.BatchSubdomain)
			admin.POST("/domains/cf-create", domainH.CFCreate)
			admin.POST("/domains/mx-import", domainH.MXImport)
			admin.POST("/domains/mx-register", domainH.MXRegister)
			admin.GET("/domains/:id/status", domainH.GetStatus)

			// 系统设置管理
			admin.GET("/settings", settingH.AdminGetAll)
			admin.PUT("/settings", settingH.AdminUpdate)
			admin.POST("/settings/tg/test", settingH.AdminTestTelegram)
			admin.GET("/hostnames", hostnameH.AdminList)
			admin.POST("/hostnames", hostnameH.Add)
			admin.PUT("/hostnames/:id", hostnameH.Update)
			admin.PUT("/hostnames/:id/toggle", hostnameH.Toggle)
			admin.DELETE("/hostnames/:id", hostnameH.Delete)
		}
	}

	// 内部邮件投递接口（Postfix pipe 调用，仅内部网络）
	internal := r.Group("/internal")
	{
		// 域名列表（供 Postfix 同步）
		internal.GET("/domains", func(c *gin.Context) {
			domains, err := db.ListDomains(c.Request.Context())
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"domains": domains})
		})

		internal.POST("/deliver", func(c *gin.Context) {
			var req struct {
				Recipient string `json:"recipient" binding:"required"`
				Sender    string `json:"sender"`
				Subject   string `json:"subject"`
				BodyText  string `json:"body_text"`
				BodyHTML  string `json:"body_html"`
				Raw       string `json:"raw"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			recipient := strings.ToLower(strings.TrimSpace(req.Recipient))
			ctx := c.Request.Context()

			// 查找收件邮箱
			mailbox, err := db.GetMailboxByFullAddress(ctx, recipient)
			if err != nil {
				if !errors.Is(err, pgx.ErrNoRows) {
					log.Printf("[deliver] lookup mailbox error for %s: %v", recipient, err)
					c.JSON(http.StatusInternalServerError, gin.H{"error": "mailbox lookup failed"})
					return
				}

				// 未知收件人 → 检查 catch-all 是否启用
				enabled, _ := db.GetSetting(ctx, "catchall_enabled")
				if enabled != "true" {
					c.JSON(http.StatusOK, gin.H{"status": "discarded", "reason": "unknown recipient"})
					return
				}

				// 解析 local-part@domain
				atIdx := strings.LastIndex(recipient, "@")
				if atIdx <= 0 || atIdx == len(recipient)-1 {
					c.JSON(http.StatusOK, gin.H{"status": "discarded", "reason": "invalid recipient"})
					return
				}
				localPart := recipient[:atIdx]
				domainPart := recipient[atIdx+1:]

				// 域名只要已录入系统即可收件：先精确匹配，未命中再对启用了
				// 多级子域名的域名做后缀匹配（xx.bb.cc.dd → base=bb.cc.dd）。
				// catch-all 仍按 base 域名落账，full_address 保留完整 recipient。
				domainRec, err := db.GetHostedDomainByName(ctx, domainPart)
				if err != nil {
					subEnabled, listErr := db.ListHostedSubdomainEnabledDomains(ctx)
					if listErr != nil || len(subEnabled) == 0 {
						c.JSON(http.StatusOK, gin.H{"status": "discarded", "reason": "domain not hosted"})
						return
					}
					names := make([]string, 0, len(subEnabled))
					for _, d := range subEnabled {
						names = append(names, d.Domain)
					}
					_, base, ok := store.SplitFullDomain(domainPart, names)
					if !ok {
						c.JSON(http.StatusOK, gin.H{"status": "discarded", "reason": "domain not hosted"})
						return
					}
					baseRec, berr := db.GetHostedDomainByName(ctx, base)
					if berr != nil {
						c.JSON(http.StatusOK, gin.H{"status": "discarded", "reason": "base domain not hosted"})
						return
					}
					domainRec = baseRec
				}

				// 决定归属账号
				ownerID, err := db.GetCatchAllAccountID(ctx)
				if err != nil {
					log.Printf("[catchall] no owner account: %v", err)
					c.JSON(http.StatusInternalServerError, gin.H{"error": "no catch-all owner account"})
					return
				}

				// 决定 TTL
				ttlMinutes := 30
				if ttlStr, err := db.GetSetting(ctx, "mailbox_ttl_minutes"); err == nil {
					if n, perr := strconv.Atoi(ttlStr); perr == nil && n > 0 {
						ttlMinutes = n
					}
				}

				// 自动建箱（并发安全）
				newMailbox, err := db.EnsureCatchAllMailbox(ctx, ownerID, localPart, domainRec.ID, recipient, ttlMinutes)
				if err != nil {
					log.Printf("[catchall] create mailbox error: %v", err)
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
				log.Printf("[catchall] auto-created mailbox %s for unknown recipient", recipient)
				mailbox = newMailbox
			}

			// 存储邮件
			email, err := db.InsertEmail(ctx,
				mailbox.ID, req.Sender, req.Subject, req.BodyText, req.BodyHTML, req.Raw)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			go forwardMailboxEmail(db, *mailbox, *email)

			c.JSON(http.StatusOK, gin.H{"status": "delivered", "email_id": email.ID})
		})
	}

	// ==================== 邮箱自动过期清理 ====================
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		log.Println("✓ Mailbox expiry cleaner started (TTL=30min, interval=1min)")
		for range ticker.C {
			if deleted, err := db.DeleteExpiredMailboxes(context.Background()); err != nil {
				log.Printf("[cleaner] error: %v", err)
			} else if deleted > 0 {
				log.Printf("[cleaner] deleted %d expired mailboxes", deleted)
			}
		}
	}()

	// ==================== MX 自动验证轮询 ====================
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		log.Println("✓ MX domain verifier started (pending check=30s, active re-check=6h)")
		reCheckTicker := time.NewTicker(6 * time.Hour)
		defer reCheckTicker.Stop()
		for {
			select {
			case <-ticker.C:
				// 处理待验证域名
				pendingDomains, err := db.ListPendingDomains(context.Background())
				if err != nil {
					log.Printf("[mx-verifier] list pending error: %v", err)
					continue
				}
				if len(pendingDomains) == 0 {
					continue
				}
				serverIP := cfg.SMTPServerIP
				if serverIP == "" {
					serverIP, _ = db.GetSetting(context.Background(), "smtp_server_ip")
				}
				for _, d := range pendingDomains {
					matched, _, mxStatus := store.CheckDomainMX(d.Domain, serverIP)
					wildcardMatched := true
					wildcardStatus := ""
					if d.SubdomainEnabled {
						wildcardMatched, _, wildcardStatus = store.CheckWildcardMX(d.Domain, serverIP)
					}
					db.TouchDomainCheckTime(context.Background(), d.ID)
					if matched && wildcardMatched {
						if err := db.PromoteDomainToActive(context.Background(), d.ID); err != nil {
							log.Printf("[mx-verifier] promote %s error: %v", d.Domain, err)
						} else {
							log.Printf("[mx-verifier] ✓ %s MX verified, domain activated", d.Domain)
						}
					} else {
						if d.SubdomainEnabled && !wildcardMatched {
							log.Printf("[mx-verifier] waiting: %s — %s | wildcard: %s", d.Domain, mxStatus, wildcardStatus)
						} else {
							log.Printf("[mx-verifier] waiting: %s — %s", d.Domain, mxStatus)
						}
					}
				}

			case <-reCheckTicker.C:
				// 每 6 小时重新检测所有已激活域名，MX 失效则自动停用
				activeDomains, err := db.GetActiveDomains(context.Background())
				if err != nil {
					log.Printf("[mx-recheck] list active error: %v", err)
					continue
				}
				serverIP := cfg.SMTPServerIP
				if serverIP == "" {
					serverIP, _ = db.GetSetting(context.Background(), "smtp_server_ip")
				}
				log.Printf("[mx-recheck] checking %d active domains", len(activeDomains))
				for _, d := range activeDomains {
					matched, _, mxStatus := store.CheckDomainMX(d.Domain, serverIP)
					db.TouchDomainCheckTime(context.Background(), d.ID)
					if !matched {
						if err := db.DisableDomainMX(context.Background(), d.ID); err != nil {
							log.Printf("[mx-recheck] disable %s error: %v", d.Domain, err)
						} else {
							log.Printf("[mx-recheck] ⚠ %s MX no longer valid (%s), domain disabled", d.Domain, mxStatus)
						}
					}
				}
			}
		}
	}()

	// ==================== 写出管理员 API Key 文件 ====================
	go func() {
		// 等待 DB 就绪后再读取（延迟 1 秒）
		time.Sleep(1 * time.Second)
		adminKey, err := db.GetAdminAPIKey(context.Background())
		if err != nil {
			log.Printf("[adminkey] could not fetch admin key: %v", err)
			return
		}
		keyFile := os.Getenv("ADMIN_KEY_FILE")
		if keyFile == "" {
			keyFile = "/data/admin.key"
		}
		if err := os.MkdirAll(filepath.Dir(keyFile), 0700); err == nil {
			content := "# TempMail Admin API Key\n# Auto-generated on startup — keep this secret!\n\nADMIN_API_KEY=" + adminKey + "\n"
			if err := os.WriteFile(keyFile, []byte(content), 0600); err != nil {
				log.Printf("[adminkey] write file error: %v", err)
			} else {
				log.Printf("✓ Admin API Key written to %s", keyFile)
			}
		}
		log.Printf("✴ ADMIN API KEY: %s", adminKey)
	}()

	// ==================== 启动服务 ====================
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("✓ API server listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	log.Println("Server exited")
}
