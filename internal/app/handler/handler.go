package handler

import (
	"net/http"

	"github.com/rs/cors"
	"github.com/sebest/xff"
	"github.com/sirupsen/logrus"
	"github.com/tranminhquanq/gomess/internal/app/repository"
	"github.com/tranminhquanq/gomess/internal/app/usecase"
	"github.com/tranminhquanq/gomess/internal/config"
	"github.com/tranminhquanq/gomess/internal/observability"
	"github.com/tranminhquanq/gomess/internal/storage"
)

const (
	audHeaderName  = "X-JWT-AUD"
	defaultVersion = "unknown version"
)

type Option interface {
	apply(*APIHandler)
}

type APIHandler struct {
	handler      http.Handler
	db           *storage.Connection
	globalConfig *config.GlobalConfiguration
	version      string
}

func NewHandler(globalConfig *config.GlobalConfiguration, db *storage.Connection, opt ...Option) *APIHandler {
	return NewHandlerWithVersion(globalConfig, db, defaultVersion, opt...)
}

func NewHandlerWithVersion(
	globalConfig *config.GlobalConfiguration,
	db *storage.Connection,
	version string,
	opt ...Option,
) *APIHandler {
	api := &APIHandler{
		globalConfig: globalConfig,
		db:           db,
		version:      version,
	}

	xffmw, _ := xff.Default()
	logger := observability.NewStructuredLogger(logrus.StandardLogger(), globalConfig)

	r := newRouter()

	r.UseBypass(observability.AddRequestID(globalConfig))
	r.UseBypass(logger)
	r.UseBypass(xffmw.Handler)
	r.UseBypass(recoverer)

	// request tracing should be added only when tracing or metrics is enabled
	// if globalConfig.Tracing.Enabled || globalConfig.Metrics.Enabled {
	// 	r.UseBypass(observability.RequestTracing())
	// }

	userRepository := repository.NewUserRepository(db)
	userUsecase := usecase.NewUserUsecase(userRepository)

	wsHandler := NewWSHandler(userUsecase)
	authHandler := NewAuthHandler(userUsecase)
	userHandler := NewUserHandler(userUsecase)

	r.Get("/health", api.HealthCheck)
	r.Get("/ws", wsHandler.HandleWS)

	r.Route("/api", func(r *router) {
		r.Route("/auth", func(r *router) {
			r.Post("/login", authHandler.Login)
			r.Post("/register", authHandler.Register)
			r.Post("/logout", authHandler.Logout)
			r.Post("/refresh", authHandler.Refresh)
			r.Post("/forgot-password", authHandler.ForgotPassword)
		})

		r.Route("/users", func(r *router) {
			r.Get("", userHandler.GetUsers)
			r.Get("/:id", userHandler.GetUser)
			r.Get("/me", userHandler.GetCurrentUser)
		})
	})

	corsHandler := cors.New(cors.Options{
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
		AllowedHeaders:   api.globalConfig.CORS.AllAllowedHeaders([]string{"Accept", "Authorization", "Content-Type", "X-Client-IP", "X-Client-Info", audHeaderName}),
		ExposedHeaders:   []string{"X-Total-Count"},
		AllowCredentials: true,
	})

	api.handler = corsHandler.Handler(r)

	return api
}

// ServeHTTP implements the http.Handler interface by passing the request along
// to its underlying Handler.
func (hdl *APIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hdl.handler.ServeHTTP(w, r)
}

type HealthCheckResponse struct {
	Version     string `json:"version"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (h *APIHandler) HealthCheck(w http.ResponseWriter, r *http.Request) error {
	return sendJSON(w, http.StatusOK, HealthCheckResponse{
		Version:     h.version,
		Name:        "GoMess",
		Description: "GoMess is a simple messaging service",
	})
}
