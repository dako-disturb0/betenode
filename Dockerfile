# Build Stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod ./
# COPY go.sum ./ (if present)
RUN go mod download

COPY . .

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /app/bete-node ./cmd/app

# Production Stage
FROM alpine:3.20

WORKDIR /home/container

RUN apk --no-cache add ca-certificates tzdata

COPY --from=builder /app/bete-node /usr/local/bin/bete-node

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/bete-node"]
