FROM golang:1.11.1 AS builder

WORKDIR /go/src/github.com/securityscorecard/dns-chief

COPY . .

RUN go build -o /out/dns-chief

################################################################################

FROM scratch

WORKDIR /app

COPY --from=builder /out/dns-chief .

ENTRYPOINT ["./dns-chief"]
