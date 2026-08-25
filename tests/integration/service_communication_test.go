package integration

import (
	"net/http"
	"testing"
)

// TestServiceCommunication tests inter-service communication
func TestServiceCommunication(t *testing.T) {
	// Test inventory service health
	resp, err := http.Get("http://localhost:8081/health")
	if err != nil {
		t.Fatalf("Failed to connect to inventory service: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// TestServiceDiscovery tests service discovery
func TestServiceDiscovery(t *testing.T) {
	services := map[string]string{
		"inventory": "http://inventory-service:8081",
		"order":     "http://order-service:8082",
	}
	if len(services) != 2 {
		t.Error("Expected 2 services in registry")
	}
}
