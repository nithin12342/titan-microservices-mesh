package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/sony/gobreaker"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	db          *gorm.DB
	redisClient *redis.Client
	cb          *gobreaker.CircuitBreaker
)

type Payment struct {
	ID            uint       `json:"id" gorm:"primaryKey"`
	OrderID       string     `json:"order_id" gorm:"index;not null"`
	UserID        string     `json:"user_id" gorm:"index;not null"`
	Amount        float64    `json:"amount" gorm:"not null"`
	Currency      string     `json:"currency" gorm:"default:USD"`
	Status        string     `json:"status" gorm:"default:pending"`
	PaymentMethod string     `json:"payment_method"`
	TransactionID string     `json:"transaction_id"`
	FailureReason string     `json:"failure_reason,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	ProcessedAt   *time.Time `json:"processed_at,omitempty"`
}

type PaymentRequest struct {
	OrderID       string  `json:"order_id" binding:"required"`
	UserID        string  `json:"user_id" binding:"required"`
	Amount        float64 `json:"amount" binding:"required,gt=0"`
	Currency      string  `json:"currency"`
	PaymentMethod string  `json:"payment_method" binding:"required"`
}

// Circuit breaker settings
func initCircuitBreaker() *gobreaker.CircuitBreaker {
	return gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        "payment-service",
		MaxRequests: 3,
		Interval:    10 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 10 && failureRatio >= 0.5
		},
	})
}

func initDB() {
	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		getEnv("DB_HOST", "postgres"),
		getEnv("DB_USER", "postgres"),
		getEnv("DB_PASSWORD", "postgres"),
		getEnv("DB_NAME", "titan_payments"),
		getEnv("DB_PORT", "5432"),
	)

	var err error
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Auto migrate
	db.AutoMigrate(&Payment{})
	log.Println("Database connected and migrated")
}

func initRedis() {
	redisClient = redis.NewClient(&redis.Options{
		Addr:     getEnv("REDIS_ADDR", "redis:6379"),
		Password: getEnv("REDIS_PASSWORD", ""),
		DB:       0,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Printf("Warning: Redis connection failed: %v", err)
	} else {
		log.Println("Redis connected")
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// Handlers

func CreatePayment(c *gin.Context) {
	var req PaymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Default currency
	if req.Currency == "" {
		req.Currency = "USD"
	}

	payment := Payment{
		OrderID:       req.OrderID,
		UserID:        req.UserID,
		Amount:        req.Amount,
		Currency:      req.Currency,
		PaymentMethod: req.PaymentMethod,
		Status:        "pending",
	}

	// Try to process payment through circuit breaker
	result, err := cb.Execute(func() (interface{}, error) {
		return processPayment(&payment)
	})

	if err != nil {
		payment.Status = "failed"
		payment.FailureReason = err.Error()
		db.Create(&payment)
		c.JSON(http.StatusPaymentRequired, gin.H{
			"error":  "Payment processing failed",
			"reason": err.Error(),
		})
		return
	}

	processedPayment := result.(*Payment)
	c.JSON(http.StatusCreated, processedPayment)
}

func processPayment(payment *Payment) (*Payment, error) {
	// Simulate payment processing
	// In production, this would call Stripe/PayPal/etc.
	time.Sleep(100 * time.Millisecond)

	// Simulate occasional failures (10% chance)
	if time.Now().UnixNano()%10 == 0 {
		return nil, fmt.Errorf("payment gateway timeout")
	}

	payment.Status = "completed"
	payment.TransactionID = fmt.Sprintf("txn_%d", time.Now().UnixNano())
	now := time.Now()
	payment.ProcessedAt = &now

	db.Create(payment)

	// Cache the payment status
	ctx := context.Background()
	redisClient.Set(ctx, fmt.Sprintf("payment:%s", payment.ID), "completed", 24*time.Hour)

	return payment, nil
}

func GetPayment(c *gin.Context) {
	id := c.Param("id")

	// Try cache first
	ctx := context.Background()
	cached, err := redisClient.Get(ctx, fmt.Sprintf("payment:%s", id)).Result()
	if err == nil {
		var payment Payment
		if json.Unmarshal([]byte(cached), &payment) == nil {
			c.JSON(http.StatusOK, payment)
			return
		}
	}

	// Fetch from database
	var payment Payment
	if result := db.First(&payment, id); result.Error != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Payment not found"})
		return
	}

	// Cache result
	if data, err := json.Marshal(payment); err == nil {
		redisClient.Set(ctx, fmt.Sprintf("payment:%s", id), data, 5*time.Minute)
	}

	c.JSON(http.StatusOK, payment)
}

func GetPaymentsByOrder(c *gin.Context) {
	orderID := c.Query("order_id")
	if orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order_id is required"})
		return
	}

	var payments []Payment
	if result := db.Where("order_id = ?", orderID).Find(&payments); result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, payments)
}

func RefundPayment(c *gin.Context) {
	id := c.Param("id")

	var payment Payment
	if result := db.First(&payment, id); result.Error != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Payment not found"})
		return
	}

	if payment.Status != "completed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Can only refund completed payments"})
		return
	}

	// Process refund
	payment.Status = "refunded"
	db.Save(&payment)

	c.JSON(http.StatusOK, payment)
}

func HealthCheck(c *gin.Context) {
	ctx := context.Background()

	// Check database
	sqlDB, err := db.DB()
	if err != nil || sqlDB.Ping() != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error":  "database unavailable",
		})
		return
	}

	// Check Redis
	if err := redisClient.Ping(ctx).Err(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error":  "redis unavailable",
		})
		return
	}

	// Check circuit breaker state
	state := cb.State()

	c.JSON(http.StatusOK, gin.H{
		"status":          "healthy",
		"circuit_breaker": state.String(),
	})
}

func main() {
	// Initialize
	initDB()
	initRedis()
	cb = initCircuitBreaker()

	// Setup router
	r := gin.Default()

	// Middleware
	r.Use(func(c *gin.Context) {
		c.Header("X-Service", "payment-service")
		c.Next()
	})

	// Routes
	r.GET("/health", HealthCheck)
	r.POST("/payments", CreatePayment)
	r.GET("/payments/:id", GetPayment)
	r.GET("/payments", GetPaymentsByOrder)
	r.POST("/payments/:id/refund", RefundPayment)

	// Start server
	port := getEnv("PORT", "8080")
	log.Printf("Starting Payment Service on port %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
