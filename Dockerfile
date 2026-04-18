FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /out/geminiweb2api ./cmd/geminiweb2api


FROM alpine:3.21

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata && update-ca-certificates

COPY --from=builder /out/geminiweb2api /app/geminiweb2api

EXPOSE 8080

VOLUME ["/app"]

CMD ["/app/geminiweb2api"]
