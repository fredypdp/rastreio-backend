package middleware

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// RateLimiter keeps independent token buckets per source address. Só as ações
// financeiras públicas o usam, com instâncias próprias. As funções
// GlobalRateLimit, LoginRateLimit e EmailRateLimit eram no-ops e foram removidas
// na Tarefa 123 (SEC-02): código morto que dava a falsa impressão de proteção.
// O risco de não haver limite de taxa em login/e-mail continua declarado no
// documento de segurança (decisão D1).
type RateLimiter struct {
	mu      sync.Mutex
	limit   rate.Limit
	burst   int
	clients map[string]*rate.Limiter
}

func NewRateLimiter(limit rate.Limit, burst int, _ time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, burst: burst, clients: map[string]*rate.Limiter{}}
}

func RateLimitMiddleware(l *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if l == nil {
			c.Next()
			return
		}
		key := c.ClientIP()
		l.mu.Lock()
		bucket := l.clients[key]
		if bucket == nil {
			bucket = rate.NewLimiter(l.limit, l.burst)
			l.clients[key] = bucket
		}
		allowed := bucket.Allow()
		l.mu.Unlock()
		if !allowed {
			c.AbortWithStatus(429)
			return
		}
		c.Next()
	}
}
