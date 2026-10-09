FROM golang:1.27.2 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/hub ./cmd/hub && CGO_ENABLED=0 go build -trimpath -o /out/hubd ./cmd/hubd

FROM debian:bookworm-slim
RUN groupadd --gid 10001 hub && useradd --uid 10001 --gid hub --no-create-home hub && mkdir /state && chown hub:hub /state && chmod 700 /state
COPY --from=build /out/hub /usr/local/bin/hub
COPY --from=build /out/hubd /usr/local/bin/hubd
USER 10001:10001
ENTRYPOINT ["hubd"]
CMD ["--fixture", "--state-dir", "/state"]
