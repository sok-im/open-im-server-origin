package options

const apnsCollapseIDMaxLen = 64

// Opts opts.
type Opts struct {
	Signal        *Signal
	IOSPushSound  string
	IOSBadgeCount bool
	Ex            string
}

// Signal message id.
type Signal struct {
	ClientMsgID string
	ServerMsgID string
}

// APNsCollapseID returns the value for the apns-collapse-id header.
// ClientMsgID is preferred; ServerMsgID is used as fallback.
func (o *Opts) APNsCollapseID() string {
	if o == nil || o.Signal == nil {
		return ""
	}
	id := o.Signal.ClientMsgID
	if id == "" {
		id = o.Signal.ServerMsgID
	}
	if len(id) > apnsCollapseIDMaxLen {
		return id[:apnsCollapseIDMaxLen]
	}
	return id
}
