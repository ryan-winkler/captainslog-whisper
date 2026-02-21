FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o captainslog ./cmd/captainslog

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/captainslog /usr/local/bin/
EXPOSE 8090
ENTRYPOINT ["captainslog"]
