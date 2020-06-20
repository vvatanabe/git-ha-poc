package proxy

import (
	"fmt"

	"google.golang.org/grpc/encoding"

	"github.com/golang/protobuf/proto"
)

// This interface is required to maintain compatibility with both grpc.Codec
// and encoding.Codec.
type Codec interface {
	Marshal(v interface{}) ([]byte, error)
	Unmarshal(data []byte, v interface{}) error
	Name() string
	String() string
}

// Codec returns a proxying encoding.Codec with the default protobuf codec as parent.
//
// See CodecWithParent.
func NewCodec() Codec {
	return CodecWithParent(&protoCodec{})
}

// CodecWithParent returns a proxying encoding.Codec with a user provided codec as parent.
//
// This codec is *crucial* to the functioning of the proxy. It allows the proxy server to be oblivious
// to the schema of the forwarded messages. It basically treats a gRPC message frame as raw bytes.
// However, if the server handler, or the client caller are not proxy-internal functions it will fall back
// to trying to decode the message using a fallback codec.
func CodecWithParent(fallback encoding.Codec) Codec {
	return &rawCodec{fallback}
}

type rawCodec struct {
	parentCodec encoding.Codec
}

type frame struct {
	payload []byte
}

func (c *rawCodec) Marshal(v interface{}) ([]byte, error) {
	out, ok := v.(*frame)
	if !ok {
		return c.parentCodec.Marshal(v)
	}
	return out.payload, nil

}

func (c *rawCodec) Unmarshal(data []byte, v interface{}) error {
	dst, ok := v.(*frame)
	if !ok {
		return c.parentCodec.Unmarshal(data, v)
	}
	dst.payload = data
	return nil
}

func (c *rawCodec) Name() string {
	return fmt.Sprintf("proxy>%s", c.parentCodec.Name())
}

// NOTE: this is required to satisfy the old grpc.Codec interface and can be
// removed as soon as it is possible to create a grpc.ServerOption with
// encoding.Codec.
func (c *rawCodec) String() string {
	return c.Name()
}

// protoCodec is a Codec implementation with protobuf. It is the default rawCodec for gRPC.
type protoCodec struct{}

func (protoCodec) Marshal(v interface{}) ([]byte, error) {
	return proto.Marshal(v.(proto.Message))
}

func (protoCodec) Unmarshal(data []byte, v interface{}) error {
	return proto.Unmarshal(data, v.(proto.Message))
}

func (protoCodec) Name() string {
	return "proto"
}

// NOTE: this is required to satisfy the old grpc.Codec interface and can be
// removed as soon as it is possible to create a grpc.ServerOption with
// encoding.Codec.
func (c protoCodec) String() string {
	return c.Name()
}
