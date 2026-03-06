# Azure Container Apps Configuration
# Titan Microservices Mesh - Container Apps Infrastructure

terraform {
  required_version = ">= 1.0"
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 3.0"
    }
  }
}

provider "azurerm" {
  features {}
}

variable "environment" {
  description = "Environment name"
  type        = string
  default     = "prod"
}

# Resource Group
resource "azurerm_resource_group" "container_apps" {
  name     = "rg-titan-containerapps-${var.environment}"
  location = "eastus"

  tags = {
    Environment = var.environment
    Project    = "titan-microservices"
  }
}

# Log Analytics Workspace
resource "azurerm_log_analytics_workspace" "container_apps_logs" {
  name                = "law-titan-${var.environment}"
  location            = azurerm_resource_group.container_apps.location
  resource_group_name = azurerm_resource_group.container_apps.name
  sku                 = "PerGB2018"
  retention_days      = 30
}

# Container Apps Environment
resource "azurerm_container_app_environment" "titan_env" {
  name                       = "cae-titan-${var.environment}"
  location                   = azurerm_resource_group.container_apps.location
  resource_group_name        = azurerm_resource_group.container_apps.name
  log_analytics_workspace_id = azurerm_log_analytics_workspace.container_apps_logs.id

  workload_profile {
    name                = "consumption"
    workload_profile_type = "Consumption"
  }

  workload_profile {
    name                  = "dedicated"
    workload_profile_type  = "D4"
    min_nodes             = 1
    max_nodes             = 3
  }
}

# Container Apps Environment - Dapr Configuration
resource "azurerm_container_app_environment_dapr_component" "state_store" {
  name         = "statestore"
  container_app_environment_id = azurerm_container_app_environment.titan_env.id
  component_type = "state.azure.blob"
  version       = "v1"

  metadata = jsonencode([
    {
      "name"  = "accountName"
      "value" = "sttitan${var.environment}"
    },
    {
      "name"  = "containerName"
      "value" = "dapr-state"
    }
  ])
}

resource "azurerm_container_app_environment_dapr_component" "pubsub" {
  name         = "pubsub"
  container_app_environment_id = azurerm_container_app_environment.titan_env.id
  component_type = "pubsub.azure.servicebus"
  version       = "v1"

  metadata = jsonencode([
    {
      "name"  = "namespaceName"
      "value" = "sb-titan-${var.environment}.servicebus.windows.net"
    }
  ])
}

# Container App - Order Service
resource "azurerm_container_app" "order_service" {
  name                         = "ca-order-service"
  container_app_environment_id = azurerm_container_app_environment.titan_env.id
  resource_group_name          = azurerm_resource_group.container_apps.location
  revision_mode               = "Single"

  workload_profile_name = "dedicated"

  container {
    name   = "order-service"
    image  = "titanregistry.azurecr.io/order-service:latest"
    
    cpu    = 0.5
    memory = "1Gi"

    env {
      name  = "DB_HOST"
      secret_ref = "db-connection"
    }
    
    env {
      name  = "REDIS_ADDR"
      secret_ref = "redis-connection"
    }

    readiness_probe {
      http_get {
        path = "/health"
        port = 8080
      }
      initial_delay_seconds = 5
      period_seconds       = 10
    }

    liveness_probe {
      http_get {
        path = "/health"
        port = 8080
      }
      initial_delay_seconds = 15
      period_seconds       = 20
    }
  }

  scale_rule {
    name = "http-rule"
    http {
      metadata = jsonencode({
        "concurrency" = "10"
      })
    }
  }

  scale_rule {
    name = "queue-rule"
    azure_queue {
      queue_name = "order-queue"
      connection = "AzureWebJobsStorage"
      metadata = jsonencode({
        "minReplicas" = "1"
        "maxReplicas" = "10"
      })
    }
  }

  ingress {
    target_port      = 8080
    external_enabled = true
    transport       = "http"
  }

  secret {
    name = "db-connection"
    value = "Server=sql-titan.database.windows.net;Database=titan_orders;User=titan_user;Password=@@PASSWORD@@"
  }

  secret {
    name = "redis-connection"
    value = "redis-titan.redis.cache.windows.net:6380,password=@@REDIS_PASSWORD@@,ssl=True,abortConnect=False"
  }

  tags = {
    Environment = var.environment
    Project    = "titan-microservices"
  }
}

# Container App - Payment Service
resource "azurerm_container_app" "payment_service" {
  name                         = "ca-payment-service"
  container_app_environment_id = azurerm_container_app_environment.titan_env.id
  resource_group_name          = azurerm_resource_group.container_apps.location
  revision_mode               = "Single"

  workload_profile_name = "dedicated"

  container {
    name   = "payment-service"
    image  = "titanregistry.azurecr.io/payment-service:latest"
    
    cpu    = 0.5
    memory = "1Gi"

    env {
      name  = "DB_HOST"
      secret_ref = "db-connection"
    }

    readiness_probe {
      http_get {
        path = "/health"
        port = 8080
      }
      initial_delay_seconds = 5
      period_seconds       = 10
    }
  }

  ingress {
    target_port      = 8080
    external_enabled = false
    transport       = "http"
  }

  secret {
    name = "db-connection"
    value = "Server=sql-titan.database.windows.net;Database=titan_payments;User=titan_user;Password=@@PASSWORD@@"
  }

  tags = {
    Environment = var.environment
    Project    = "titan-microservices"
  }
}

# Container App - Inventory Service
resource "azurerm_container_app" "inventory_service" {
  name                         = "ca-inventory-service"
  container_app_environment_id = azurerm_container_app_environment.titan_env.id
  resource_group_name          = azurerm_resource_group.container_apps.location
  revision_mode               = "Single"

  workload_profile_name = "dedicated"

  container {
    name   = "inventory-service"
    image  = "titanregistry.azurecr.io/inventory-service:latest"
    
    cpu    = 0.5
    memory = "1Gi"

    env {
      name  = "DB_HOST"
      secret_ref = "db-connection"
    }

    readiness_probe {
      http_get {
        path = "/health"
        port = 8082
      }
      initial_delay_seconds = 5
      period_seconds       = 10
    }
  }

  ingress {
    target_port      = 8082
    external_enabled = false
    transport       = "http"
  }

  secret {
    name = "db-connection"
    value = "Server=sql-titan.database.windows.net;Database=titan_inventory;User=titan_user;Password=@@PASSWORD@@"
  }

  tags = {
    Environment = var.environment
    Project    = "titan-microservices"
  }
}

# Azure Service Bus Namespace
resource "azurerm_servicebus_namespace" "titan_sb" {
  name                = "sb-titan-${var.environment}"
  location            = azurerm_resource_group.container_apps.location
  resource_group_name = azurerm_resource_group.container_apps.name
  sku                 = "Standard"
  zone_redundant      = true

  tags = {
    Environment = var.environment
    Project    = "titan-microservices"
  }
}

# Service Bus Queue - Orders
resource "azurerm_servicebus_queue" "order_queue" {
  name                = "order-queue"
  namespace_id        = azurerm_servicebus_namespace.titan_sb.id
  max_message_size_kb = 1024
  lock_duration       = "PT1M"
}

# Service Bus Topic - Notifications
resource "azurerm_servicebus_topic" "notifications" {
  name                = "notifications"
  namespace_id        = azurerm_servicebus_namespace.titan_sb.id
  max_message_size_kb = 1024
}

# Outputs
output "container_apps_env_id" {
  value = azurerm_container_app_environment.titan_env.id
}

output "order_service_url" {
  value = azurerm_container_app.order_service.configuration.ingress.fqdn
}
