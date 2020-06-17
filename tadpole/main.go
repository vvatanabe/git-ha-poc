package main

import (
	"context"

	pbSmart "github.com/vvatanabe/git-ha-poc/proto/smart"
)

func main() {

}

type SmartProtocolService struct {
}

func (s *SmartProtocolService) PostUploadPack(request *pbSmart.UploadPackRequest, server pbSmart.SmartProtocolService_PostUploadPackServer) error {
	panic("implement me")
}

func (s *SmartProtocolService) PostReceivePack(server pbSmart.SmartProtocolService_PostReceivePackServer) error {
	panic("implement me")
}

func (s *SmartProtocolService) GetInfoRefs(ctx context.Context, request *pbSmart.InfoRefsRequest) (*pbSmart.InfoRefsResponse, error) {
	panic("implement me")
}
