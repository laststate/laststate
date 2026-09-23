FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /billing-service ./cmd/billing-service

FROM alpine:3.21
RUN apk --no-cache add ca-certificates \
  && addgroup -S billing && adduser -S -G billing billing \
  && mkdir -p /app && chown -R billing:billing /app
WORKDIR /app
COPY --from=builder /billing-service .
RUN chown billing:billing /app/billing-service
EXPOSE 8080
USER billing
CMD ["/app/billing-service"]
