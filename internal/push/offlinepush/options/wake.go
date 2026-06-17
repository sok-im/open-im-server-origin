package options

// IsWakePush reports whether the push carries SOK wake ex payload for client-side decryption.
func (o *Opts) IsWakePush() bool {
	return o != nil && o.Ex != ""
}

// WakeExtras builds EngageLab/JPush extras for wake push: ex and sok_wake_push carry the same JSON string.
func WakeExtras(o *Opts) map[string]interface{} {
	extras := make(map[string]interface{})
	if o == nil || o.Ex == "" {
		return extras
	}
	extras["ex"] = o.Ex
	extras["sok_wake_push"] = o.Ex
	if o.Signal != nil && o.Signal.ClientMsgID != "" {
		extras["ClientMsgID"] = o.Signal.ClientMsgID
	}
	return extras
}
