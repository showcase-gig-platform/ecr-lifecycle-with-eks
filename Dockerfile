FROM public.ecr.aws/docker/library/golang:1.23-alpine AS builder

WORKDIR /workdir

COPY go.mod ./
COPY go.sum ./

RUN go mod download

COPY main.go ./

ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -a -o ecr-lifecycle-with-eks main.go

FROM public.ecr.aws/docker/library/alpine:3.19

RUN apk add --no-cache ca-certificates && \
    addgroup -g 65532 -S appgroup && \
    adduser -u 65532 -S appuser -G appgroup

WORKDIR /
COPY --from=builder /workdir/ecr-lifecycle-with-eks .

USER appuser:appgroup

ENTRYPOINT ["/ecr-lifecycle-with-eks"]
