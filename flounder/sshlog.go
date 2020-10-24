package main

import (
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

type ChannelWithSize struct {
	ssh.Channel
	size int64
}

func (ch *ChannelWithSize) Size() int64 {
	return ch.size
}

func (ch *ChannelWithSize) Write(data []byte) (int, error) {
	written, err := ch.Channel.Write(data)
	ch.size += int64(written)
	return written, err
}

type GitSSHLogger struct {
	sugar *zap.SugaredLogger
}

func (z *GitSSHLogger) Infof(format string, args ...interface{}) {
	z.sugar.Infof(format, args...)
}

func (z *GitSSHLogger) Errorf(format string, args ...interface{}) {
	z.sugar.Errorf(format, args...)
}

func (z *GitSSHLogger) Fatalf(format string, args ...interface{}) {
	z.sugar.Fatalf(format, args...)
}
