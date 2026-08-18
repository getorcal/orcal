FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION}" -o /out/orcald ./cmd/orcald
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION}" -o /out/orcal ./cmd/orcal

FROM alpine:3.20
RUN adduser -D -u 10001 orcal && mkdir -p /var/lib/orcal && chown orcal /var/lib/orcal
COPY --from=build /out/orcald /usr/local/bin/orcald
COPY --from=build /out/orcal /usr/local/bin/orcal
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/orcald"]
