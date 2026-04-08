FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN go mod tidy

ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/otel-sample-app ./main.go

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/otel-sample-app /app/otel-sample-app

ENV PORT=8080

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/app/otel-sample-app"]
