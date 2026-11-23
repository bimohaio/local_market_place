FROM golang:1.25-alpine AS builder

WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
WORKDIR /app
RUN mkdir -p /app/uploads && chown 10001:10001 /app/uploads
COPY --from=builder /out/api ./api
EXPOSE 8080
USER 10001:10001
ENTRYPOINT ["./api"]
