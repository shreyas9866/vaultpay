# VaultPay 🏦

![Coverage](https://img.shields.io/badge/Coverage-72.7%25-brightgreen.svg)
![Go Version](https://img.shields.io/badge/Go-1.26-blue.svg)
![Docker](https://img.shields.io/badge/Docker-Distroless-blue.svg)

A highly concurrent, production-ready Payment Gateway API built with Go, PostgreSQL, and Redis.

## 🏗 System Architecture
```mermaid
graph TD
    Client([Client]) -->|HTTP Requests| RateLimiter{Redis Rate Limiter}
    RateLimiter -->|Pass| API[Go API Server]
    RateLimiter -->|429| Client

    API -->|ACID Transactions| DB[(PostgreSQL)]
    API -->|Transaction Event| Outbox[(Outbox Table)]

    Outbox -->|Poll| Worker[Background Webhook Worker]
    Worker -->|HMAC Signed HTTP POST| Merchant([Merchant Server])
    Worker -.->|Update Retries / Backoff| Outbox