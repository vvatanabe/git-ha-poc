FROM golang:1.14.4

ENV TZ Asia/Tokyo
COPY swordfish/config.yml .
RUN go get github.com/vvatanabe/grpc-proxyd

ENTRYPOINT ["/go/bin/grpc-proxyd"]