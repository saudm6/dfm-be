# syntax=docker/dockerfile:1

FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dfm ./cmd/dfm

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/dfm /dfm
ENV DFM_HTTP_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/dfm"]
