package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"local-marketplace/backend/internal/config"
	"local-marketplace/backend/internal/platform"
	httptransport "local-marketplace/backend/internal/transport/http"
)

func main() {
	cfg := config.Load()
	if len(cfg.JWTSecret) < 32 {
		log.Fatal("JWT_SECRET must be at least 32 characters")
	}

	db, err := platform.NewPostgres(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect postgres: %v", err)
	}
	defer db.Close()

	rdb := platform.NewRedis(cfg.RedisAddr, cfg.RedisPassword)
	defer rdb.Close()

	router := gin.Default()
	if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
		log.Fatalf("create upload directory: %v", err)
	}
	router.Static("/uploads", cfg.UploadDir)
	httptransport.RegisterRoutes(router, db, rdb)
	auth := httptransport.NewAuth(db, cfg.JWTSecret)
	if err := auth.EnsureSchema(); err != nil {
		log.Fatalf("initialize auth schema: %v", err)
	}
	auth.RegisterRoutes(router.Group("/api/v1"))
	catalog := httptransport.NewCatalog(db, cfg.UploadDir)
	if err := catalog.EnsureSchema(); err != nil {
		log.Fatalf("initialize catalog schema: %v", err)
	}
	catalog.RegisterRoutes(router.Group("/api/v1"), auth)
	orders := httptransport.NewOrders(db)
	if err := orders.EnsureSchema(); err != nil {
		log.Fatalf("initialize order schema: %v", err)
	}
	orders.RegisterRoutes(router.Group("/api/v1"), auth)
	go orders.RunReservationCleanup(context.Background())
	chat := httptransport.NewChat(db, rdb)
	if err := chat.EnsureSchema(); err != nil {
		log.Fatalf("initialize chat schema: %v", err)
	}
	chat.RegisterRoutes(router.Group("/api/v1"), auth)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("API listening on :%s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown: %v", err)
	}
}
