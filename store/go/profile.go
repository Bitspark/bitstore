package bitstore

import (
	"context"
	"fmt"
)

// BytesBackendProfile identifies the raw bytes adapter, independently of Data's
// codec profile. An advertisement promises the complete raw Store contract.
const BytesBackendProfile = "bitstore/bytes"
const BytesBackendVersion = "1"
const BytesHTTPBinding = "bitstore-http/2026-09-24"

// BackendProfile is an optional /describe extension. Unsupported versions and
// bindings are refused, never inferred from a URL, service name or hostname.
type BackendProfile struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Binding string `json:"binding"`
}

func BytesProfile() BackendProfile {
	return BackendProfile{BytesBackendProfile, BytesBackendVersion, BytesHTTPBinding}
}

// DiscoverBytes validates the explicit compatible profile before returning the
// existing HTTP Store adapter. The configured endpoint prefix is preserved.
// Management endpoints are supplied independently and cannot pass this check.
func DiscoverBytes(ctx context.Context, endpoint string, opts ...Option) (*Client, error) {
	c, err := New(endpoint, opts...)
	if err != nil {
		return nil, err
	}
	d, err := c.Describe(ctx)
	if err != nil {
		return nil, err
	}
	var found bool
	for _, p := range d.Profiles {
		if p.ID == BytesBackendProfile {
			if found || p != BytesProfile() {
				return nil, fmt.Errorf("%w: incompatible or duplicate bytes profile", ErrProtocol)
			}
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: bytes profile not advertised", ErrProtocol)
	}
	if d.Service != "bitstore" || (d.Surface != "" && d.Surface != "operations") || d.Contract != Contract || c.ServedContract() != Contract || d.Bindings.Primary != "http" || d.Bindings.HTTP.Prefix != "/v1" {
		return nil, fmt.Errorf("%w: bytes profile contradicts describe envelope", ErrProtocol)
	}
	return c, nil
}
