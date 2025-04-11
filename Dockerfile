FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY main.go .
COPY go.mod .
COPY go.sum* .

RUN if [ ! -f go.mod ]; then \
    go mod init outline_exporter && \
    go mod tidy; \
    fi

RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o outline-exporter .

FROM alpine:3.19

WORKDIR /app

RUN apk --no-cache add ca-certificates tzdata

COPY --from=builder /app/outline-exporter /app/outline-exporter

EXPOSE 9877

RUN adduser -D -H -u 10001 exporter
USER exporter

ENTRYPOINT ["/app/outline-exporter"]
