package app

import "context"

// DNSBackend is the in-process boundary used by selection and management.
// Policy application still validates and persists the snapshot before activation.
type DNSBackend interface {
	Apply(context.Context, Policy) error
	Check(context.Context, string) (DNSCheck, error)
}

type DNSCheck struct {
	Domain     string   `json:"domain"`
	RCode      string   `json:"rcode"`
	Answers    []string `json:"answers"`
	DurationMS int64    `json:"duration_ms"`
}
