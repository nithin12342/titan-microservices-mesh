# Titan Microservices Mesh

Distributed systems and service mesh meta-repository.

## Project Structure

```
titan-microservices-mesh/
├── services/
│   ├── order-service/        # Order management
│   ├── payment-service/     # Payment processing
│   ├── inventory-service/  # Inventory management
│   ├── user-service/       # User management
│   ├── analytics-service/  # Analytics
│   └── notification-service/ # Notifications
├── mesh/
│   ├── istio-config/       # Istio service mesh
│   └── nginx-ingress/      # Ingress configuration
├── templates/              # Service templates
└── helm/                  # Helm charts
```

## Technology Stack

- **Languages**: Go, Rust, Java, Node.js, Python
- **Service Mesh**: Istio
- **Databases**: PostgreSQL, Cosmos DB, MongoDB, Redis, ClickHouse
- **Messaging**: Azure Service Bus, RabbitMQ, NATS
- **API Gateway**: Azure API Management

## Getting Started

### Prerequisites
- Kubernetes cluster (AKS)
- Istio
- Helm

### Local Development

```bash
# Start all services
docker-compose up -d

# Or use Skaffold
skaffold dev
```

### Deployment

```bash
# Deploy to Kubernetes
kubectl apply -f ./mesh/
```

## License

Proprietary - Dulux Tech
