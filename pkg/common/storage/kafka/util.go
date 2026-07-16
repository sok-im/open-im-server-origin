package kafka

import (
	"context"
	"errors"

	"github.com/IBM/sarama"
	"github.com/openimsdk/open-im-server/v3/pkg/common/otelx"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/mcontext"
)

var errEmptyMsg = errors.New("kafka binary msg is empty")

// GetMQHeaderWithContext extracts message queue headers from the context.
func GetMQHeaderWithContext(ctx context.Context) ([]sarama.RecordHeader, error) {
	operationID, opUserID, platform, connID, err := mcontext.GetCtxInfos(ctx)
	if err != nil {
		return nil, err
	}
	return otelx.InjectTraceHeaders(ctx, []sarama.RecordHeader{
		{Key: []byte(constant.OperationID), Value: []byte(operationID)},
		{Key: []byte(constant.OpUserID), Value: []byte(opUserID)},
		{Key: []byte(constant.OpUserPlatform), Value: []byte(platform)},
		{Key: []byte(constant.ConnID), Value: []byte(connID)},
	}), nil
}

// GetContextWithMQHeader creates a context from message queue headers.
func GetContextWithMQHeader(header []*sarama.RecordHeader) context.Context {
	var operationID, opUserID, platform, connID string
	for _, recordHeader := range header {
		if recordHeader == nil {
			continue
		}
		switch string(recordHeader.Key) {
		case constant.OperationID:
			operationID = string(recordHeader.Value)
		case constant.OpUserID:
			opUserID = string(recordHeader.Value)
		case constant.OpUserPlatform:
			platform = string(recordHeader.Value)
		case constant.ConnID:
			connID = string(recordHeader.Value)
		}
	}
	ctx := mcontext.WithMustInfoCtx([]string{operationID, opUserID, platform, connID})
	return otelx.ExtractTraceContext(ctx, header)
}
