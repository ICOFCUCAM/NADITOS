# Multi-stage build for any Go service in /services/<SERVICE>.
# Build context: repository root.
ARG SERVICE
FROM golang:1.25-alpine AS build
ARG SERVICE
WORKDIR /src
COPY packages/go-common ./packages/go-common
COPY services/${SERVICE} ./services/${SERVICE}
WORKDIR /src/services/${SERVICE}
RUN go mod download && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8000-9000
USER nonroot
ENTRYPOINT ["/server"]
