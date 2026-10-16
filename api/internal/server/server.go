// Package server assembles the Gin engine (routes + middleware chain).
package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/cattyman919/athenaeum/api/internal/config"
	"github.com/cattyman919/athenaeum/api/internal/handler"
	"github.com/cattyman919/athenaeum/api/internal/middleware"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/pkg/jwt"
	"github.com/cattyman919/athenaeum/api/internal/service/admin"
	"github.com/cattyman919/athenaeum/api/internal/service/ai"
	"github.com/cattyman919/athenaeum/api/internal/service/auth"
	"github.com/cattyman919/athenaeum/api/internal/service/catalog"
	"github.com/cattyman919/athenaeum/api/internal/service/circulation"
	"github.com/cattyman919/athenaeum/api/internal/service/library"
	"github.com/cattyman919/athenaeum/api/internal/service/waitlist"
)

// Deps are the collaborators wired in cmd/api (manual DI, docs/01 §2).
// Optional members may be nil in tests: nil JWT/Auth skip identity and auth
// routes, nil Limiter disables rate limiting.
type Deps struct {
	Config   config.Config
	Log      *slog.Logger
	JWT      *jwt.Manager
	Auth     *auth.Service
	Catalog  *catalog.Service
	Library  *library.Service
	Waitlist *waitlist.Service
	Chat     *ai.Service
	// Transcribe serves voice input (docs/15); nil skips the route.
	Transcribe *ai.TranscribeService
	// Admin serves /api/admin/* (docs/03 §8); nil skips the routes.
	Admin *admin.Service
	// ISBN serves barcode intake (docs/11); nil skips the route.
	ISBN *catalog.ISBNService
	// Scan resolves patron barcode scans (docs/03 §5.4); nil skips the route.
	Scan *catalog.ScanService
	// Circulation serves patron self-service return (docs/03 §6.4); nil skips the route.
	Circulation *circulation.Service
	Limiter     *middleware.RateLimiter
	// ChatHeartbeat overrides the SSE heartbeat interval (tests); 0 = 15 s.
	ChatHeartbeat time.Duration
}

// specificBucketRoutes shadow the general 120/min bucket (docs/03 §2).
var specificBucketRoutes = map[string]bool{
	"POST /api/auth/login":        true,
	"POST /api/auth/register":     true,
	"GET /api/books/search":       true,
	"POST /api/chat":              true,
	"POST /api/chat/transcribe":   true,
	"POST /api/books/fetch-isbn":  true,
	"POST /api/books/scan":        true,
	"POST /api/copies/:id/return": true,
}

func isSpecific(c *gin.Context) bool {
	return specificBucketRoutes[c.Request.Method+" "+c.FullPath()]
}

// NewRouter builds the engine with the shared middleware chain and all routes.
func NewRouter(d Deps) *gin.Engine {
	if d.Config.IsProd() {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(
		middleware.RequestID(),
		middleware.Recover(d.Log),
		middleware.AccessLog(d.Log),
		middleware.CORS(d.Config.WebOrigin),
	)
	if d.JWT != nil {
		r.Use(middleware.Identify(d.JWT))
	}
	r.NoRoute(func(c *gin.Context) {
		handler.RespondError(c, model.New(model.CodeNotFound, "Route not found."))
	})

	api := r.Group("/api")
	if d.Limiter != nil {
		api.Use(d.Limiter.Limit(middleware.BucketGeneral, handler.RespondError, isSpecific))
	}
	api.GET("/health", handler.Health)

	requireAuth := middleware.RequireAuth(handler.RespondError)
	limit := func(b middleware.Bucket) gin.HandlerFunc {
		if d.Limiter == nil {
			return func(c *gin.Context) { c.Next() }
		}
		return d.Limiter.Limit(b, handler.RespondError, nil)
	}

	if d.Auth != nil {
		ah := handler.NewAuthHandler(d.Auth, d.Config.IsProd(), d.Config.WebOrigin, []byte(d.Config.JWTRefreshSecret))
		ag := api.Group("/auth")
		ag.POST("/register", limit(middleware.BucketRegister), ah.Register)
		ag.POST("/login", limit(middleware.BucketLogin), ah.Login)
		ag.POST("/refresh", ah.Refresh)
		ag.POST("/logout", requireAuth, ah.Logout)
		ag.GET("/me", requireAuth, ah.Me)
		ag.GET("/google", ah.GoogleStart)
		ag.GET("/google/callback", ah.GoogleCallback)
	}

	if d.Catalog != nil {
		bh := handler.NewBooksHandler(d.Catalog)
		bg := api.Group("/books", requireAuth)
		bg.GET("/search", limit(middleware.BucketSearch), bh.Search)
		bg.GET("/:id", bh.Get)
	}

	if d.ISBN != nil {
		ih := handler.NewISBNHandler(d.ISBN)
		// Admin "Add Book" intake (docs/11 §1); dedicated 10/min bucket (docs/03 §2).
		api.POST("/books/fetch-isbn", requireAuth, middleware.RequireAdmin(handler.RespondError),
			limit(middleware.BucketFetchISBN), ih.FetchISBN)
	}

	if d.Scan != nil || d.Circulation != nil {
		sh := handler.NewScanHandler(d.Scan, d.Circulation)
		// Patron scanner (docs/11 §9); dedicated buckets shadow the general one (docs/03 §2).
		if d.Scan != nil {
			api.POST("/books/scan", requireAuth, limit(middleware.BucketScan), sh.Scan)
		}
		if d.Circulation != nil {
			api.POST("/copies/:id/return", requireAuth, limit(middleware.BucketCopyReturn), sh.Return)
		}
	}

	if d.Library != nil && d.Waitlist != nil {
		lh := handler.NewLibraryHandler(d.Library, d.Waitlist)
		lg := api.Group("/library", requireAuth)
		lg.GET("", lh.List)
		lg.POST("", lh.Add)
		lg.PATCH("/:book_id", lh.Patch)
		lg.DELETE("/:book_id", lh.Remove)
		wg := api.Group("", requireAuth)
		wg.POST("/books/:id/waitlist", lh.JoinWaitlist)
		wg.DELETE("/books/:id/waitlist", lh.LeaveWaitlist)
		wg.GET("/waitlist", lh.ListWaitlist)
	}

	if d.Chat != nil {
		ch := handler.NewChatHandler(d.Chat, d.ChatHeartbeat)
		cg := api.Group("/chat", requireAuth)
		// chat_day last: its X-RateLimit-* headers win (daily quota meter, docs/06 §8).
		cg.POST("", limit(middleware.BucketChatMin), limit(middleware.BucketChatDay), ch.Stream)
		cg.GET("/conversations", ch.List)
		cg.POST("/conversations", ch.Create)
		cg.GET("/conversations/:id", ch.Get)
		cg.DELETE("/conversations/:id", ch.Delete)
	}

	if d.Transcribe != nil {
		th := handler.NewTranscribeHandler(d.Transcribe)
		// Final clips use the 10/min bucket; live partials (?partial=true, docs/15 §2a)
		// use their own 60/min bucket plus a one-in-flight-per-user guard.
		notPartial := func(c *gin.Context) bool { return !handler.IsPartialTranscribe(c) }
		chain := []gin.HandlerFunc{requireAuth}
		if d.Limiter != nil {
			chain = append(chain,
				d.Limiter.Limit(middleware.BucketTranscribe, handler.RespondError, handler.IsPartialTranscribe),
				d.Limiter.Limit(middleware.BucketTranscribePartial, handler.RespondError, notPartial),
				d.Limiter.SingleFlight("transcribe_partial", ai.PartialTranscribeTimeout+5*time.Second,
					handler.RespondError, notPartial),
			)
		}
		api.POST("/chat/transcribe", append(chain, th.Transcribe)...)
	}

	if d.Admin != nil {
		adm := handler.NewAdminHandler(d.Admin)
		ad := api.Group("/admin", requireAuth, middleware.RequireAdmin(handler.RespondError))
		ad.GET("/data", adm.Data)
		ad.GET("/copy-events", adm.CopyEvents)
		ad.GET("/books", adm.ListBooks)
		ad.POST("/books", adm.CreateBook)
		ad.PATCH("/books/:id", adm.PatchBook)
		ad.DELETE("/books/:id", adm.DeleteBook)
		ad.POST("/copies", adm.AddCopy)
		ad.PATCH("/copies/:id", adm.PatchCopy)
		ad.DELETE("/copies/:id", adm.DeleteCopy)
		ad.GET("/tree", adm.Tree)
		ad.GET("/shelves/:id", adm.Shelf)
		ad.GET("/users", adm.Users)
		ad.PATCH("/users/:id", adm.PatchUser)
	}
	return r
}

// New returns the http.Server for the given address.
func New(d Deps, addr string) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           NewRouter(d),
		ReadHeaderTimeout: 5 * time.Second,
	}
}
