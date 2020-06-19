FROM golang:1.14.4

ENV PKG_PATH /go/src/github.com/vvatanabe/git-ha-poc
ENV SERVICE_PATH $PKG_PATH/tadpole
ENV GO111MODULE on
WORKDIR $PKG_PATH
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . $PKG_PATH
RUN GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go install $SERVICE_PATH

ENV TZ Asia/Tokyo
RUN mkdir -p /root/git/repositories
ENTRYPOINT ["/go/bin/tadpole"]