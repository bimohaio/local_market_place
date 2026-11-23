package http

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"local-marketplace/backend/internal/platform"
)

func RegisterRoutes(router *gin.Engine, db *sql.DB, redisClient *redis.Client) {
	api := router.Group("/api/v1")
	api.GET("/health", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		dbStatus := "ok"
		if err := db.PingContext(ctx); err != nil {
			dbStatus = "unavailable"
		}

		redisStatus := "ok"
		if err := platform.PingRedis(ctx, redisClient); err != nil {
			redisStatus = "unavailable"
		}

		status := http.StatusOK
		if dbStatus != "ok" || redisStatus != "ok" {
			status = http.StatusServiceUnavailable
		}

		c.JSON(status, gin.H{
			"status": "ok",
			"dependencies": gin.H{
				"postgres": dbStatus,
				"redis":    redisStatus,
			},
		})
	})
}
