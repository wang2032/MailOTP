# syntax=docker/dockerfile:1

FROM node:22-alpine AS web-builder
WORKDIR /src/apps/web
COPY apps/web/package.json apps/web/package-lock.json ./
RUN npm ci
COPY apps/web ./
RUN npm run build

FROM golang:1.23-alpine AS api-builder
WORKDIR /src/apps/api
COPY apps/api/go.mod apps/api/go.sum ./
RUN go mod download
COPY apps/api ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mailotp-api ./cmd/api

FROM alpine:3.21
WORKDIR /app
RUN adduser -D -H -u 10001 mailotp
COPY --from=api-builder /out/mailotp-api /app/mailotp-api
COPY --from=web-builder /src/apps/web/dist /app/web
ENV HTTP_ADDR=:8080
ENV STATIC_DIR=/app/web
ENV AUTO_CREATE_TABLES=true
EXPOSE 8080
USER mailotp
CMD ["/app/mailotp-api"]
