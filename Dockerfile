FROM public.ecr.aws/docker/library/golang:1.18 AS builder

WORKDIR /workdir

COPY go.mod ./
COPY go.sum ./

RUN go mod download

COPY main.go ./

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o ecr-lifecycle-with-eks main.go

FROM scratch

WORKDIR /
COPY --from=builder /workdir/ecr-lifecycle-with-eks .
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

ENTRYPOINT ["/ecr-lifecycle-with-eks"]
