## Multi-stage Dockerfile for otel-fintrans-simulator
# Builds a static linux/amd64 binary and creates a minimal final image.

FROM golang:1.24 as builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Build a static linux amd64 binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o /app/otel-fintrans-simulator

FROM scratch
COPY --from=builder /app/otel-fintrans-simulator /otel-fintrans-simulator
ENTRYPOINT ["/otel-fintrans-simulator"]
