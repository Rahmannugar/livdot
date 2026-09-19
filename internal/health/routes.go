package health

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const dependencyCheckTimeout = 2 * time.Second

func RegisterRoutes(router gin.IRoutes, database *pgxpool.Pool, cache *redis.Client) {
	router.GET("/health/live", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/health/ready", func(ctx *gin.Context) {
		checkContext, cancel := context.WithTimeout(ctx.Request.Context(), dependencyCheckTimeout)
		defer cancel()

		if err := database.Ping(checkContext); err != nil {
			ctx.JSON(http.StatusServiceUnavailable, gin.H{
				"status":     "unavailable",
				"dependency": "postgres",
			})
			return
		}
		if err := cache.Ping(checkContext).Err(); err != nil {
			ctx.JSON(http.StatusServiceUnavailable, gin.H{
				"status":     "unavailable",
				"dependency": "redis",
			})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
}
