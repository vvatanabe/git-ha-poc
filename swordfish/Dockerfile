FROM golang:1.14.4

ENV PKG_PATH /go/src/github.com/vvatanabe/git-ha-poc
ENV SERVICE_PATH $PKG_PATH/swordfish
ENV GO111MODULE on
WORKDIR $PKG_PATH
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . $PKG_PATH
RUN GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go install $SERVICE_PATH

FROM alpine:3.12
ENV TZ Asia/Tokyo
COPY --from=0 /go/bin/swordfish /go/bin/swordfish

ENV WAIT_VERSION 2.7.2
ADD https://github.com/ufoscout/docker-compose-wait/releases/download/$WAIT_VERSION/wait /wait
RUN chmod +x /wait

CMD /wait && /go/bin/swordfish