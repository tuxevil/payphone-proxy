FROM golang:1.24 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/payphone-proxy ./cmd/payphone-proxy
RUN mkdir -p /runtime-data && chown 65532:65532 /runtime-data

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --chown=nonroot:nonroot --from=build /out/payphone-proxy /app/payphone-proxy
COPY --chown=nonroot:nonroot --from=build /runtime-data /data
VOLUME ["/data"]
ENV HTTP_ADDR=:8080
ENV DATABASE_PATH=/data/payments.db
EXPOSE 8080
ENTRYPOINT ["/app/payphone-proxy"]
