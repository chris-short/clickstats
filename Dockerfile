FROM golang:1.22-alpine AS builder
WORKDIR /build
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o clickstats .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /build/clickstats /usr/local/bin/clickstats
EXPOSE 8080
CMD ["clickstats", "serve", "--host=0.0.0.0", "--port=8080"]
