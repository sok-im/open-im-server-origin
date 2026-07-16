package prommetrics

import (
	"testing"
	"time"
)

func TestRPCObserve(t *testing.T) {
	RPCObserve("msg", "/openim.msg.msg/SendMsg", 0, 12*time.Millisecond)
	RPCObserve("msg", "/openim.msg.msg/SendMsg", 13, 30*time.Millisecond)
}

func TestWSConnRejectedReasons(t *testing.T) {
	SetWSMaxConn(10000)
	SetWSConnectionNum(12)
	WSConnRejected(WSRejectReasonMaxConn)
	WSConnRejected(WSRejectReasonAuth)
}
