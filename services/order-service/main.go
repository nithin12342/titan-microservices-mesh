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
	"github.com/sony/gobreaker"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/trace"
)

type Order struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	TotalAmount float64   `json:"total_amount"`
	Status      string    `json:"status"`
	Items       []Item    `json:"items"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Item struct {
	ProductID string  `json:"product_id"`
	Name      string  `json:"name"`
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price"`
}

type OrderService struct {
	cb *gobreaker.CircuitBreaker
}

func NewOrderService() *OrderService {
	settings := gobreaker.Settings{
		Name:        "order-service",
		MaxRequests: 3,
		Interval:    10 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 10 && failureRatio >= 0.5
		},
	}

	return &OrderService{
		cb: gobreaker.NewCircuitBreaker(settings),
	}
}

func (s *OrderService) CreateOrder(c *gin.Context) {
	var order Order
	if err := c.ShouldBindJSON(&order); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	order.ID = generateID()
	order.Status = "pending"
	order.CreatedAt = time.Now()
	order.UpdatedAt = time.Now()

	// Call inventory service
	if err := s.checkInventory(order.Items); err != nil {
		order.Status = "failed"
		c.JSON(http.StatusBadRequest, gin.H{"error": "inventory check failed", "details": err.Error()})
		return
	}

	// Call payment service
	if err := s.processPayment(order.UserID, order.TotalAmount); err != nil {
		order.Status = "payment_failed"
		c.JSON(http.StatusPaymentRequired, gin.H{"error": "payment failed", "details": err.Error()})
		return
	}

	order.Status = "confirmed"

	c.JSON(http.StatusCreated, order)
}

func (s *OrderService) GetOrder(c *gin.Context) {
	orderID := c.Param("id")

	// Mock response
	order := Order{
		ID:          orderID,
		UserID:      "user-123",
		TotalAmount: 99.99,
		Status:      "confirmed",
		Items: []Item{
			{ProductID: "prod-1", Name: "Product 1", Quantity: 2, Price: 49.99},
		},
		CreatedAt: time.Now().Add(-1 * time.Hour),
		UpdatedAt: time.Now(),
	}

	c.JSON(http.StatusOK, order)
}

func (s *OrderService) checkInventory(items []Item) error {
	// Circuit breaker wrapped call
	result, err := s.cb.Execute(func() (interface{}, error) {
		// In production, call inventory service
		// resp, err := http.Post("http://inventory-service/check", "application/json", ...)
		return true, nil
	})

	if err != nil {
		return err
	}
	return nil
}

func (s *OrderService) processPayment(userID string, amount float64) error {
	// Circuit breaker wrapped call
	result, err := s.cb.Execute(func() (interface{}, error) {
		// In production, call payment service
		// resp, err := http.Post("http://payment-service/charge", "application/json", ...)
		return true, nil
	})

	if err != nil {
		return err
	}
	return nil
}

func generateID() string {
	return time.Now().Format("20060102150405") + "-" + randomString(8)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
	}
	return string(b)
}

func setupTracing() {
	exporter, _ := stdouttrace.New(stdouttrace.WithPrettyPrint())
	tp := trace.NewTracerProvider(trace.WithBatcher(exporter))
	otel.SetTracerProvider(tp)
}

func main() {
	// Setup
	setupTracing()

	router := gin.Default()

	// Middleware
	router.Use(func(c *gin.Context) {
		c.Header("X-Request-ID", generateID())
		c.Next()
	})

	service := NewOrderService()

	// Routes
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})

	router.POST("/api/v1/orders", service.CreateOrder)
	router.GET("/api/v1/orders/:id", service.GetOrder)

	// Server
	srv := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	go func() {
		log.Printf("Order service starting on port 8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server exiting")
}
