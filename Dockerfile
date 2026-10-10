# syntax=docker/dockerfile:1.8
FROM golang:1.26.5-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/http-server ./cmd/http-server && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/webhook-sink ./cmd/webhook-sink

FROM alpine:3.22
RUN apk add --no-cache ca-certificates wget && addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=build /out/ /usr/local/bin/
COPY migrations/ ./migrations/
USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/http-server"]
