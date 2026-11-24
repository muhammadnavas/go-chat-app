FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o /app/server ./cmd/server

FROM alpine:3.20

WORKDIR /app
RUN apk add --no-cache ca-certificates

COPY --from=builder /app/server /app/server
COPY web ./web

EXPOSE 8080

CMD ["/app/server"]

