package pikpak

import (
	"net/http"
	"time"
)

const (
	// DefaultClientID is the public client identifier used by the official CLI.
	DefaultClientID      = "aal7sToC85zMXEpJ"
	DefaultClientVersion = "1.0.0"

	DefaultBaseUser  = "https://user.mypikpak.com"
	DefaultBaseDrive = "https://api-drive.mypikpak.com"

	// Scope is the full set v1 needs, including files:purge so permanent
	// delete works without a second consent.
	Scope = "files:read files:write files:manage files:purge cloud_download share account"
)

type ClientOptions struct {
	BaseUser      string
	BaseDrive     string
	ClientID      string
	ClientVersion string
	DeviceID      string
	HTTP          *http.Client
	Session       TokenSource
}

// Client talks to PikPak. One instance is per fnOS user once a Session is
// attached; a Session-less instance is only used to start the device flow.
type Client struct {
	BaseUser      string
	BaseDrive     string
	ClientID      string
	ClientVersion string
	DeviceID      string
	HTTP          *http.Client

	// Session is set by SessionStore.ClientFor; requests sign with it.
	Session TokenSource

	limiter *limiter
}

func NewClient(o ClientOptions) *Client {
	if o.BaseUser == "" {
		o.BaseUser = DefaultBaseUser
	}
	if o.BaseDrive == "" {
		o.BaseDrive = DefaultBaseDrive
	}
	if o.ClientID == "" {
		o.ClientID = DefaultClientID
	}
	if o.ClientVersion == "" {
		o.ClientVersion = DefaultClientVersion
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		BaseUser:      o.BaseUser,
		BaseDrive:     o.BaseDrive,
		ClientID:      o.ClientID,
		ClientVersion: o.ClientVersion,
		DeviceID:      o.DeviceID,
		HTTP:          o.HTTP,
		Session:       o.Session,
		limiter:       newLimiter(),
	}
}
