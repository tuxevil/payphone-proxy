FROM golang:1.25.13@sha256:cbff9d1a9041b316010f2da6b701b6c0d597718cb90928c85eb597334a0d23d4 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/payphone-proxy ./cmd/payphone-proxy
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/payphone-proxy-healthcheck ./cmd/payphone-proxy-healthcheck
RUN install -d -m 0750 -o 65532 -g 65532 /runtime-data

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

WORKDIR /app
COPY --chown=nonroot:nonroot --from=build /out/payphone-proxy /app/payphone-proxy
COPY --chown=nonroot:nonroot --from=build /out/payphone-proxy-healthcheck /app/payphone-proxy-healthcheck
COPY --chown=nonroot:nonroot --from=build /runtime-data /data
VOLUME ["/data"]
ENV HTTP_ADDR=:8080
ENV DATABASE_PATH=/data/payments.db
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/app/payphone-proxy-healthcheck"]
ENTRYPOINT ["/app/payphone-proxy"]
