FROM golang:1.15.3

ENV NAME protoc-go-grpc
ENV PROTOBUF_VERSION 3.13.0
ENV PROTOBUF_ZIP protoc-${PROTOBUF_VERSION}-linux-x86_64.zip
ENV PROTOBUF_URL https://github.com/protocolbuffers/protobuf/releases/download/v$PROTOBUF_VERSION/$PROTOBUF_ZIP

RUN apt-get update && \
    apt-get install unzip && \
    curl -OL $PROTOBUF_URL && \
    unzip -o $PROTOBUF_ZIP -d /usr/local bin/protoc && \
    unzip -o $PROTOBUF_ZIP -d /usr/local include/* && \
    chmod +x /usr/local/bin/protoc && \
    rm -rf $PROTOBUF_ZIP && \
    export GO111MODULE=on && \
    go get -u \
        google.golang.org/protobuf/cmd/protoc-gen-go \
        google.golang.org/grpc/cmd/protoc-gen-go-grpc && \
    mkdir /$NAME

WORKDIR /$NAME

ENTRYPOINT ["protoc"]
CMD ["--help"]