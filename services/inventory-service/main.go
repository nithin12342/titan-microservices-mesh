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
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	db          *gorm.DB
	redisClient *redis.Client
)

type Product struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	SKU          string    `json:"sku" gorm:"uniqueIndex;not null"`
	Name         string    `json:"name" gorm:"not null"`
	Description  string    `json:"description"`
	Category     string    `json:"category"`
	Price        float64   `json:"price" gorm:"not null"`
	Quantity     int       `json:"quantity" gorm:"default:0"`
	ReservedQty  int       `json:"reserved_qty" gorm:"default:0"`
	AvailableQty int       `json:"available_qty" gorm:"-"`
	Warehouse    string    `json:"warehouse"`
	IsActive     bool      `json:"is_active" gorm:"default:true"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type InventoryRequest struct {
	SKU    string `json:"sku" binding:"required"`
	Delta  int    `json:"delta" binding:"required"` // positive for add, negative for remove
	Reason string `json:"reason"`
}

type ReserveRequest struct {
	Items []ReserveItem `json:"items" binding:"required"`
}

type ReserveItem struct {
	SKU      string `json:"sku" binding:"required"`
	Quantity int    `json:"quantity" binding:"required,gt=0"`
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func initDB() {
	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		getEnv("DB_HOST", "postgres"),
		getEnv("DB_USER", "postgres"),
		getEnv("DB_PASSWORD", "postgres"),
		getEnv("DB_NAME", "titan_inventory"),
		getEnv("DB_PORT", "5432"),
	)

	var err error
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	db.AutoMigrate(&Product{})
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

// Handlers

func CreateProduct(c *gin.Context) {
	var product Product
	if err := c.ShouldBindJSON(&product); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if result := db.Create(&product); result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	c.JSON(http.StatusCreated, product)
}

func GetProduct(c *gin.Context) {
	sku := c.Param("sku")

	// Try cache first
	ctx := context.Background()
	cached, err := redisClient.Get(ctx, fmt.Sprintf("product:%s", sku)).Result()
	if err == nil {
		var product Product
		if json.Unmarshal([]byte(cached), &product) == nil {
			c.JSON(http.StatusOK, product)
			return
		}
	}

	// Fetch from database
	var product Product
	if result := db.Where("sku = ?", sku).First(&product); result.Error != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}

	product.AvailableQty = product.Quantity - product.ReservedQty

	// Cache result
	if data, err := json.Marshal(product); err == nil {
		redisClient.Set(ctx, fmt.Sprintf("product:%s", sku), data, 5*time.Minute)
	}

	c.JSON(http.StatusOK, product)
}

func ListProducts(c *gin.Context) {
	var products []Product
	query := db.Where("is_active = ?", true)

	if category := c.Query("category"); category != "" {
		query = query.Where("category = ?", category)
	}

	if result := query.Find(&products); result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	// Calculate available quantity
	for i := range products {
		products[i].AvailableQty = products[i].Quantity - products[i].ReservedQty
	}

	c.JSON(http.StatusOK, products)
}

func UpdateInventory(c *gin.Context) {
	var req InventoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var product Product
	if result := db.Where("sku = ?", req.SKU).First(&product); result.Error != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}

	newQty := product.Quantity + req.Delta
	if newQty < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Insufficient inventory"})
		return
	}

	product.Quantity = newQty
	db.Save(&product)

	// Invalidate cache
	ctx := context.Background()
	redisClient.Del(ctx, fmt.Sprintf("product:%s", req.SKU))

	c.JSON(http.StatusOK, gin.H{
		"sku":           product.SKU,
		"quantity":      product.Quantity,
		"available_qty": product.Quantity - product.ReservedQty,
	})
}

func ReserveInventory(c *gin.Context) {
	var req ReserveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Use database transaction
	tx := db.Begin()

	reserved := []map[string]interface{}{}
	errors := []string{}

	for _, item := range req.Items {
		var product Product
		if result := tx.Where("sku = ?", item.SKU).First(&product); result.Error != nil {
			tx.Rollback()
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Product %s not found", item.SKU)})
			return
		}

		available := product.Quantity - product.ReservedQty
		if available < item.Quantity {
			tx.Rollback()
			c.JSON(http.StatusBadRequest, gin.H{
				"error":     fmt.Sprintf("Insufficient inventory for %s", item.SKU),
				"available": available,
				"requested": item.Quantity,
			})
			return
		}

		product.ReservedQty += item.Quantity
		tx.Save(&product)

		reserved = append(reserved, map[string]interface{}{
			"sku":      product.SKU,
			"reserved": item.Quantity,
		})
	}

	tx.Commit()

	c.JSON(http.StatusOK, gin.H{
		"status":   "reserved",
		"items":    reserved,
		"order_id": fmt.Sprintf("res_%d", time.Now().Unix()),
	})
}

func ReleaseReservation(c *gin.Context) {
	sku := c.Param("sku")

	var product Product
	if result := db.Where("sku = ?", sku).First(&product); result.Error != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}

	if product.ReservedQty > 0 {
		product.ReservedQty -= 1 // Release 1 unit
		db.Save(&product)
	}

	// Invalidate cache
	ctx := context.Background()
	redisClient.Del(ctx, fmt.Sprintf("product:%s", sku))

	c.JSON(http.StatusOK, gin.H{
		"sku":           product.SKU,
		"reserved_qty":  product.ReservedQty,
		"available_qty": product.Quantity - product.ReservedQty,
	})
}

func GetInventoryStats(c *gin.Context) {
	var totalProducts, totalQuantity, totalReserved int64

	db.Model(&Product{}).Count(&totalProducts)
	db.Model(&Product{}).Select("COALESCE(SUM(quantity), 0)").Row().Scan(&totalQuantity)
	db.Model(&Product{}).Select("COALESCE(SUM(reserved_qty), 0)").Row().Scan(&totalReserved)

	c.JSON(http.StatusOK, gin.H{
		"total_products":  totalProducts,
		"total_quantity":  totalQuantity,
		"total_reserved":  totalReserved,
		"total_available": totalQuantity - totalReserved,
	})
}

func HealthCheck(c *gin.Context) {
	ctx := context.Background()

	sqlDB, err := db.DB()
	if err != nil || sqlDB.Ping() != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "error": "database unavailable"})
		return
	}

	if err := redisClient.Ping(ctx).Err(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "error": "redis unavailable"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "healthy"})
}

func main() {
	initDB()
	initRedis()

	r := gin.Default()

	r.GET("/health", HealthCheck)
	r.POST("/products", CreateProduct)
	r.GET("/products/:sku", GetProduct)
	r.GET("/products", ListProducts)
	r.PUT("/inventory", UpdateInventory)
	r.POST("/inventory/reserve", ReserveInventory)
	r.POST("/inventory/release/:sku", ReleaseReservation)
	r.GET("/inventory/stats", GetInventoryStats)

	port := getEnv("PORT", "8082")
	log.Printf("Starting Inventory Service on port %s", port)
	r.Run(":" + port)
}
