package rtc

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/open-im-server/v3/pkg/util/conversationutil"
	pbrtc "github.com/openimsdk/protocol/rtc"
)

func parseE2EEFromCustomData(customData string) (required bool, callID, e2eeJSON string, err error) {
	if strings.TrimSpace(customData) == "" {
		return false, "", "", nil
	}
	var outer map[string]json.RawMessage
	if err := json.Unmarshal([]byte(customData), &outer); err != nil {
		return false, "", "", err
	}
	raw, ok := outer["e2ee"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return false, "", "", nil
	}
	var descriptor struct {
		Required bool   `json:"required"`
		CallID   string `json:"callID"`
	}
	if err := json.Unmarshal(raw, &descriptor); err != nil {
		return false, "", "", err
	}
	return descriptor.Required, descriptor.CallID, string(raw), nil
}

func normalizeCallConversationID(groupID, inviterID string, inviteeIDs []string) string {
	if groupID != "" {
		return groupID
	}
	if len(inviteeIDs) == 0 {
		return ""
	}
	return conversationutil.GenConversationIDForSingle(inviterID, inviteeIDs[0])
}

func checkE2EECapability(cap *pbrtc.E2EECapability, allowedSchemes []string, minVersion int) error {
	if cap == nil || !cap.GetFrameCryptor() {
		return servererrs.ErrCallE2EERequiredUnsupported.Wrap()
	}
	allowed := make(map[string]struct{}, len(allowedSchemes))
	for _, scheme := range allowedSchemes {
		allowed[scheme] = struct{}{}
	}
	for _, scheme := range cap.GetSchemes() {
		if _, ok := allowed[scheme]; ok {
			if version, ok := capabilityMajorVersion(cap.GetClientVersion()); ok && version < minVersion {
				return servererrs.ErrCallE2EEProtocolVersionMismatch.Wrap()
			}
			return nil
		}
	}
	return servererrs.ErrCallE2EEProtocolVersionMismatch.Wrap()
}

func capabilityMajorVersion(value string) (int, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if value == "" {
		return 0, false
	}
	part, _, _ := strings.Cut(value, ".")
	version, err := strconv.Atoi(part)
	return version, err == nil
}

func e2eeRequiredFromInvitation(inv *model.SignalInvitation) bool {
	return inv != nil && inv.E2EERequired
}
