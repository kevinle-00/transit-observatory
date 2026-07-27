FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S transit && adduser -S -G transit -h /app transit
WORKDIR /app
COPY --from=build --chown=transit:transit /out/api /out/worker /app/
USER transit
ENV PORT=8080
EXPOSE 8080
CMD ["/app/api"]
