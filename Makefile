CURRENT_UID := $(shell id -u)
CURRENT_GID := $(shell id -g)

.PHONY: protobuf
protobuf-go:
	docker run -it --rm \
      -v /etc/passwd:/etc/passwd:ro \
      -v $(PWD):/protoc-go-grpc \
      -u $(CURRENT_UID):$(CURRENT_GID) \
      ghcr.io/vvatanabe/protoc-go-grpc-docker/protoc-go-grpc:v0.0.1 \
      -I=proto \
        --go_out=./proto --go_opt=paths=source_relative \
        --go-grpc_out=./proto --go-grpc_opt=paths=source_relative \
        ./proto/*/*.proto