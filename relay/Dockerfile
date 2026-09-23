# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.cliVersion=${VERSION} -X main.gitCommit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
    -o /out/laststate-relay ./cmd/laststate-relay

FROM alpine:3.21
RUN addgroup -S laststate && adduser -S -G laststate laststate \
    && mkdir -p /var/lib/laststate /etc/laststate \
    && chown -R laststate:laststate /var/lib/laststate /etc/laststate
COPY --from=build /out/laststate-relay /usr/local/bin/laststate-relay
USER laststate
VOLUME ["/var/lib/laststate"]
EXPOSE 8383 8384 9467
ENTRYPOINT ["laststate-relay"]
CMD ["run", "--config", "/etc/laststate/relay.yaml"]
