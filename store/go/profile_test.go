package bitstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	bs "github.com/Bitspark/bitstore/store/go"
)

func TestBytesDiscovery(t *testing.T) {
	for _, kind := range []string{"compatible", "explicit operations", "missing", "duplicate", "version", "binding", "envelope", "surface", "header contract"} {
		t.Run(kind, func(t *testing.T) {
			d := bs.Describe{Service: "bitstore", Contract: bs.Contract, Bindings: bs.Bindings{Primary: "http", HTTP: bs.BindingHTTP{Prefix: "/v1"}}, Profiles: []bs.BackendProfile{bs.BytesProfile()}}
			switch kind {
			case "explicit operations":
				d.Surface = "operations"
			case "missing":
				d.Profiles = nil
			case "duplicate":
				d.Profiles = append(d.Profiles, bs.BytesProfile())
			case "version":
				d.Profiles[0].Version = "2"
			case "binding":
				d.Profiles[0].Binding = "unknown"
			case "envelope":
				d.Contract = "future"
			case "surface":
				d.Surface = "management"
			}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/deployment/describe" {
					t.Errorf("prefix lost: %s", r.URL.Path)
				}
				w.Header().Set("Bit-Service", "bitstore")
				w.Header().Set("Bit-Contract", bs.Contract)
				if kind == "header contract" {
					w.Header().Set("Bit-Contract", "future")
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(d)
			}))
			defer s.Close()
			_, err := bs.DiscoverBytes(context.Background(), s.URL+"/deployment")
			if kind == "compatible" || kind == "explicit operations" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, bs.ErrProtocol) {
				t.Fatal("accepted incompatible discovery", err)
			}
		})
	}
}
