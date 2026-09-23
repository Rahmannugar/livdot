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

		// check every dependency so the response shows which one is unhealthy.
		dependencies := map[string]string{
			"postgres": checkPostgres(checkContext, database),
			"redis":    checkRedis(checkContext, cache),
		}
		status := http.StatusOK
		state := "ready"
		for _, result := range dependencies {
			if result != "ok" {
				status = http.StatusServiceUnavailable
				state = "unavailable"
				break
			}
		}
		ctx.JSON(status, gin.H{"status": state, "dependencies": dependencies})
	})
}

func checkPostgres(ctx context.Context, database *pgxpool.Pool) string {
	if err := database.Ping(ctx); err != nil {
		return "unavailable"
	}
	return "ok"
}

func checkRedis(ctx context.Context, cache *redis.Client) string {
	if err := cache.Ping(ctx).Err(); err != nil {
		return "unavailable"
	}
	return "ok"
}
