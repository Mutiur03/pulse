# Multi-stage Go build
FROM golang:1.26-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git make

COPY go/go.mod go/go.sum ./
RUN go mod download

COPY go/ ./

RUN CGO_ENABLED=0 GOOS=linux go build -o /ingest-gateway ./cmd/ingest-gateway
RUN CGO_ENABLED=0 GOOS=linux go build -o /stream-processor ./cmd/stream-processor
RUN CGO_ENABLED=0 GOOS=linux go build -o /query-api ./cmd/query-api
RUN CGO_ENABLED=0 GOOS=linux go build -o /alerting-worker ./cmd/alerting-worker

# Ingest gateway
FROM alpine:latest AS ingest-gateway
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /ingest-gateway .
EXPOSE 8080
CMD ["./ingest-gateway"]

# Stream processor
FROM alpine:latest AS stream-processor
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /stream-processor .
EXPOSE 8082
CMD ["./stream-processor"]

# Query API
FROM alpine:latest AS query-api
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /query-api .
EXPOSE 8081
CMD ["./query-api"]

# Alerting worker
FROM alpine:latest AS alerting-worker
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /alerting-worker .
EXPOSE 8083
CMD ["./alerting-worker"]
