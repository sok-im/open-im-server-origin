// Copyright © 2023 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package prommetrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	WSRejectReasonMaxConn   = "max_conn"
	WSRejectReasonParseArgs = "parse_args"
	WSRejectReasonAuth      = "auth_failed"
	WSRejectReasonValidate  = "validate_failed"
	WSRejectReasonUpgrade   = "upgrade_failed"
)

var (
	OnlineUserGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "online_user_num",
		Help: "The number of online users",
	})
	OnlineUserConnGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_connection_num",
		Help: "The number of active websocket connections",
	})
	WSMaxConnGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_max_connection_num",
		Help: "Configured websocket max connection limit",
	})
	WSConnRejectedCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ws_conn_rejected_total",
			Help: "Websocket connections rejected before established",
		},
		[]string{"reason"},
	)
)

func WSConnRejected(reason string) {
	WSConnRejectedCounter.WithLabelValues(reason).Inc()
}

func SetWSMaxConn(max int64) {
	WSMaxConnGauge.Set(float64(max))
}

func SetWSConnectionNum(n int64) {
	OnlineUserConnGauge.Set(float64(n))
}
