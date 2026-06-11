package body

type PushRequest struct {
	From      string `json:"from,omitempty"`
	To        To     `json:"to"`
	Body      Body   `json:"body"`
	RequestID string `json:"request_id,omitempty"`
}

type To struct {
	Alias []string `json:"alias,omitempty"`
}

type Body struct {
	Platform     string        `json:"platform"`
	Notification *Notification `json:"notification,omitempty"`
	Options      *Options      `json:"options,omitempty"`
}

type Options struct {
	ApnsProduction bool `json:"apns_production"`
}

type Notification struct {
	Alert   string  `json:"alert,omitempty"`
	Android Android `json:"android,omitempty"`
	IOS     IOS     `json:"ios,omitempty"`
}

type Android struct {
	Alert  string `json:"alert,omitempty"`
	Title  string `json:"title,omitempty"`
	Intent struct {
		URL string `json:"url,omitempty"`
	} `json:"intent,omitempty"`
	Extras map[string]string `json:"extras,omitempty"`
}

type IOS struct {
	Alert          any               `json:"alert,omitempty"`
	Sound          string            `json:"sound,omitempty"`
	Badge          string            `json:"badge,omitempty"`
	Extras         map[string]string `json:"extras,omitempty"`
	MutableContent bool              `json:"mutable-content"`
}

type IOSAlert struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}
