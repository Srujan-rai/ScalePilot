# Build from repository root:
#   eval $(minikube docker-env)
#   docker build -f hack/minikube-demo/Dockerfile -t web-frontend-metrics:demo .
FROM golang:1.24-bookworm AS build
ENV GOTOOLCHAIN=auto
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /metrics-demo ./cmd/metrics-demo

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /metrics-demo /metrics-demo
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/metrics-demo"]
