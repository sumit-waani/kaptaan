FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git gcc musl-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o kaptaan ./cmd/kaptaan

# ── Runtime image ─────────────────────────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates git github-cli bash

WORKDIR /app

COPY --from=builder /app/kaptaan .

# Workspace dir for cloned repos
RUN mkdir -p /tmp/kaptaan-workspace

CMD ["./kaptaan"]
