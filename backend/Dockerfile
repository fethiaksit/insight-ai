FROM golang:1.26-alpine AS build
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /api ./cmd/api
FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget && adduser -D -u 10001 appuser
WORKDIR /app
RUN mkdir -p data/documents && chown -R appuser:appuser /app
USER appuser
COPY --from=build /api /api
EXPOSE 8080
ENTRYPOINT ["/api"]
