FROM golang:1.14.4

ENV TZ Asia/Tokyo
COPY config.yml .
RUN go get -u go get github.com/vvatanabe/grpc-proxyd

ENTRYPOINT ["/go/bin/user"]