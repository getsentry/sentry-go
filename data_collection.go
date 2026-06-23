package sentry

// CollectionMode controls how key-value data (headers, cookies, query params) is collected.
type CollectionMode string

const (
	// CollectionOff disables collection of the category entirely.
	CollectionOff CollectionMode = "off"

	// CollectionDenyList keeps all keys and filters denied values.
	CollectionDenyList CollectionMode = "denyList"

	// CollectionAllowList keeps all keys and sends real values only for allowed
	// keys; all other values are filtered.
	CollectionAllowList CollectionMode = "allowList"
)

// KeyValueCollectionBehavior configures how key-value data is collected and filtered.
type KeyValueCollectionBehavior struct {
	// Mode controls the collection strategy. Default: CollectionDenyList.
	Mode CollectionMode
	// Terms is a list of additional terms used by the active mode.
	Terms []string
}

// HeaderCollectionConfig configures how HTTP headers are collected for
// requests and responses independently.
type HeaderCollectionConfig struct {
	// Request configures collection of HTTP request headers. Defaults to DenyList.
	Request *KeyValueCollectionBehavior
	// Response configures collection of HTTP response headers. Defaults to DenyList.
	Response *KeyValueCollectionBehavior
}

// BodyType identifies a category of HTTP body to collect.
type BodyType string

const (
	// BodyIncomingRequest collects bodies from incoming HTTP requests
	// (server-side).
	BodyIncomingRequest BodyType = "incomingRequest"

	// BodyOutgoingRequest collects bodies from outgoing HTTP requests
	// (client-side).
	BodyOutgoingRequest BodyType = "outgoingRequest"

	// BodyIncomingResponse collects bodies from incoming HTTP responses
	// (client-side).
	BodyIncomingResponse BodyType = "incomingResponse"

	// BodyOutgoingResponse collects bodies from outgoing HTTP responses
	// (server-side).
	BodyOutgoingResponse BodyType = "outgoingResponse"
)

// DataCollection configures what data the SDK collects automatically.
// All fields are optional. nil or zero-value fields use the documented
// defaults, which collect rich context for debugging while scrubbing sensitive
// values via a built-in denylist.
//
// See https://docs.sentry.io/platforms/go/configuration/options/#DataCollection
type DataCollection struct {
	// UserInfo controls automatic population of user.* fields from auto-instrumentation.
	//
	// This does NOT gate data explicitly set via Scope.SetUser(); that is
	// always attached. Defaults to true.
	UserInfo Option[bool]

	// Cookies configures collection of HTTP cookies.
	//
	// Defaults to using the built-in DenyList.
	Cookies *KeyValueCollectionBehavior

	// HTTPHeaders configures collection of HTTP request and response headers
	// independently.
	//
	// Defaults to both request and response using the built-in DenyList.
	HTTPHeaders *HeaderCollectionConfig

	// HTTPBodies controls which HTTP body types are collected.
	//
	// Defaults to collecting all valid body types.
	HTTPBodies []BodyType

	// QueryParams configures collection of URL query parameters.
	//
	// Defaults to using the built-in DenyList.
	QueryParams *KeyValueCollectionBehavior
}

// cloneKeyValueCollectionBehavior returns a deep copy of b.
func cloneKeyValueCollectionBehavior(b *KeyValueCollectionBehavior) *KeyValueCollectionBehavior {
	if b == nil {
		return nil
	}
	cloned := &KeyValueCollectionBehavior{Mode: b.Mode}
	if b.Terms != nil {
		cloned.Terms = append([]string(nil), b.Terms...)
	}
	return cloned
}

// cloneHeaderCollectionConfig returns a deep copy of c.
func cloneHeaderCollectionConfig(c *HeaderCollectionConfig) *HeaderCollectionConfig {
	if c == nil {
		return nil
	}
	return &HeaderCollectionConfig{
		Request:  cloneKeyValueCollectionBehavior(c.Request),
		Response: cloneKeyValueCollectionBehavior(c.Response),
	}
}

// cloneDataCollection returns a deep copy of dc.
func cloneDataCollection(dc *DataCollection) *DataCollection {
	if dc == nil {
		return nil
	}
	cloned := &DataCollection{
		UserInfo:    dc.UserInfo,
		Cookies:     cloneKeyValueCollectionBehavior(dc.Cookies),
		HTTPHeaders: cloneHeaderCollectionConfig(dc.HTTPHeaders),
		QueryParams: cloneKeyValueCollectionBehavior(dc.QueryParams),
	}
	if dc.HTTPBodies != nil {
		cloned.HTTPBodies = append([]BodyType{}, dc.HTTPBodies...)
	}
	return cloned
}

// defaultKeyValueBehavior returns the default key-value collection behavior.
func defaultKeyValueBehavior() *KeyValueCollectionBehavior {
	return &KeyValueCollectionBehavior{Mode: CollectionDenyList}
}

// allBodyTypes returns all valid body types.
func allBodyTypes() []BodyType {
	return []BodyType{
		BodyIncomingRequest,
		BodyOutgoingRequest,
		BodyIncomingResponse,
		BodyOutgoingResponse,
	}
}

// newDataCollection builds a fully-populated DataCollection by applying
// defaults to any nil fields. It also handles backward compatibility with the
// legacy SendDefaultPII option.
func newDataCollection(dc *DataCollection, sendDefaultPII bool) DataCollection {
	var resolved DataCollection
	if cloned := cloneDataCollection(dc); cloned != nil {
		resolved = *cloned
	}

	isZero := dc == nil || (!resolved.UserInfo.IsSet &&
		resolved.Cookies == nil &&
		resolved.HTTPHeaders == nil &&
		resolved.HTTPBodies == nil &&
		resolved.QueryParams == nil)

	// TODO: should consider sendDefaultPII on how to apply and use DataCollection for
	// backward compatibility. Will be used in a next step.
	_ = isZero && sendDefaultPII

	if !resolved.UserInfo.IsSet {
		resolved.UserInfo = Set(true)
	}
	if resolved.Cookies == nil {
		resolved.Cookies = defaultKeyValueBehavior()
	}
	if resolved.HTTPHeaders == nil {
		resolved.HTTPHeaders = &HeaderCollectionConfig{}
	}
	if resolved.HTTPHeaders.Request == nil {
		resolved.HTTPHeaders.Request = defaultKeyValueBehavior()
	}
	if resolved.HTTPHeaders.Response == nil {
		resolved.HTTPHeaders.Response = defaultKeyValueBehavior()
	}
	if resolved.HTTPBodies == nil {
		resolved.HTTPBodies = allBodyTypes()
	}
	if resolved.QueryParams == nil {
		resolved.QueryParams = defaultKeyValueBehavior()
	}
	return resolved
}

// isHTTPBodyCollected reports whether the given body type should be collected
// according to the DataCollection configuration.
func (dc *DataCollection) isHTTPBodyCollected(bt BodyType) bool {
	if dc == nil || dc.HTTPBodies == nil {
		return true
	}
	for _, t := range dc.HTTPBodies {
		if t == bt {
			return true
		}
	}
	return false
}
