package message

import (
	"context"
	"strings"
)

type StaticLinkPolicy struct {
	Hosts   []string
	Version string
}

func (p StaticLinkPolicy) AllowedDestinationHosts(context.Context) ([]string, string, error) {
	out := make([]string, 0, len(p.Hosts))
	for _, h := range p.Hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h != "" {
			out = append(out, h)
		}
	}
	v := strings.TrimSpace(p.Version)
	if v == "" {
		v = "static-v1"
	}
	return out, v, nil
}
