package bitstore_test

import (
	"testing"

	"github.com/Bitspark/bitstore/conformance/go"
	bitstore "github.com/Bitspark/bitstore/store/go"
)

func TestMemoryStore(t *testing.T) {
	conformance.RunStore(t, func(t *testing.T) conformance.Subject {
		return conformance.Subject{Store: bitstore.NewMemory(2 << 20), MaxBytes: 2 << 20}
	})
}

func TestMemoryStoreWithoutACap(t *testing.T) {
	conformance.RunStore(t, func(t *testing.T) conformance.Subject {
		return conformance.Subject{Store: bitstore.NewMemory(0)}
	})
}

func TestParseName(t *testing.T) {
	good := string(bitstore.NameOf([]byte("x")))
	if _, err := bitstore.ParseName(good); err != nil {
		t.Errorf("%s: %v", good, err)
	}
	for _, bad := range []string{"", "sha256:", good[:len(good)-1], "SHA256" + good[6:], "sha256:" + string(make([]byte, 64)), good + "0", "sha512:" + good[7:]} {
		if _, err := bitstore.ParseName(bad); err == nil {
			t.Errorf("%q parsed as a name", bad)
		}
	}
}
