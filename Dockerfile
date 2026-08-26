FROM golang:1.25-bookworm AS build

WORKDIR /src
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/robotcell-server ./cmd/server
RUN mkdir -p /out/data

FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /app
COPY --from=build /out/robotcell-server /app/robotcell-server
COPY --from=build --chown=65532:65532 /out/data /data
VOLUME ["/data"]
ENV ROBOTCELL_ADDR=:8080 \
    ROBOTCELL_DB_PATH=/data/robotcell.db
EXPOSE 8080
HEALTHCHECK --interval=5s --timeout=2s --start-period=5s --retries=12 CMD ["/app/robotcell-server", "healthcheck"]
ENTRYPOINT ["/app/robotcell-server"]
