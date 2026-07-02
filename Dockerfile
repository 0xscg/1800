# Multi-stage: build web, build Go, ship one small image that serves both.

FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.22-alpine AS backend
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api && \
    CGO_ENABLED=0 go build -o /out/migrate ./cmd/migrate

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=backend /out/api /out/migrate ./
COPY --from=web /web/dist ./web-dist
COPY backend/migrations ./migrations
ENV WEB_DIST=/app/web-dist
EXPOSE 8080
CMD ["/app/api"]
