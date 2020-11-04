package main

import (
	"context"

	pbUser "github.com/vvatanabe/git-ha-poc/proto/user"
)

type UserService struct {
	pbUser.UnimplementedUserServiceServer
}

func (u UserService) CreateUser(ctx context.Context, request *pbUser.CreateUserRequest) (*pbUser.CreateUserResponse, error) {
	panic("implement me")
}
