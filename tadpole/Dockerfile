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

FROM alpine/git:v2.26.2
ENV TZ Asia/Tokyo
RUN mkdir -p /root/git/repositories
COPY --from=0 /go/bin/tadpole /go/bin/tadpole
RUN mkdir -p /root/.ssh && echo "Host *" > /root/.ssh/config && echo " StrictHostKeyChecking no" >> /root/.ssh/config
RUN ssh-keygen -t rsa -b 4096 -f ./host_key -q -P ""
RUN ssh-keygen -t rsa -b 4096 -f /root/.ssh/id_rsa -q -P ""

ENTRYPOINT ["/go/bin/tadpole"]