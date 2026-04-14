package rpcli

import (
	"github.com/openimsdk/protocol/crypto"
	"google.golang.org/grpc"
)

func NewCryptoServiceClient(cc grpc.ClientConnInterface) *CryptoServiceClient {
	return &CryptoServiceClient{crypto.NewCryptoServiceClient(cc)}
}

type CryptoServiceClient struct {
	crypto.CryptoServiceClient
}
