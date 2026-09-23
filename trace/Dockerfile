FROM node:20-bookworm AS webbuild
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/trace ./cmd/trace

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
  && rm -rf /var/lib/apt/lists/* \
  && groupadd -r trace && useradd -r -g trace -d /app -s /usr/sbin/nologin trace \
  && mkdir -p /app /data/objects && chown -R trace:trace /app /data
WORKDIR /app
COPY --from=build /out/trace /app/trace
COPY --from=webbuild /web/dist /app/web/dist
RUN chown -R trace:trace /app
ENV TRACE_WEB_DIR=/app/web/dist
ENV TRACE_OBJECT_DIR=/data/objects
EXPOSE 8080
USER trace
ENTRYPOINT ["/app/trace"]
