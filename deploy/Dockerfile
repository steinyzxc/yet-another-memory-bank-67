FROM golang:1.26-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN mkdir -p /out /empty-var-lib-mcb \
    && CGO_ENABLED=1 go build -tags sqlite_fts5 -trimpath -ldflags="-s -w" -o /out/mcb ./cmd/mcb

FROM gcr.io/distroless/cc-debian12

COPY --from=build /out/mcb /usr/local/bin/mcb
COPY --from=build --chown=10001:10001 /empty-var-lib-mcb /var/lib/mcb

USER 10001:10001
EXPOSE 3411
VOLUME ["/var/lib/mcb"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD ["/usr/local/bin/mcb", "healthz"]

ENTRYPOINT ["/usr/local/bin/mcb"]
CMD ["serve"]
