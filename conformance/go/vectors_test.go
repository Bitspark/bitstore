package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"testing"
)

var nameGrammar = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Every vector's name and size are recomputed from its content, so the file
// cannot state a fact about SHA-256 that is not one.
func TestVectorsHoldForSHA256(t *testing.T) {
	vectors, err := Vectors()
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) == 0 {
		t.Fatal("no vectors")
	}
	seen := map[string]string{}
	for _, v := range vectors {
		t.Run(v.Comment, func(t *testing.T) {
			if v.Comment == "" {
				t.Error("a vector without a comment")
			}
			if !nameGrammar.MatchString(v.Name) {
				t.Errorf("name %q is outside the grammar", v.Name)
			}
			if other, dup := seen[v.Name]; dup {
				t.Errorf("name %s is also vector %q", v.Name, other)
			}
			seen[v.Name] = v.Comment

			content, err := v.Content.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			if int64(len(content)) != v.Size {
				t.Errorf("size %d, content has %d bytes", v.Size, len(content))
			}
			sum := sha256.Sum256(content)
			if got := "sha256:" + hex.EncodeToString(sum[:]); got != v.Name {
				t.Errorf("name %s, content hashes to %s", v.Name, got)
			}
		})
	}
}

func TestContentForms(t *testing.T) {
	empty, text := "", "aGk="
	cases := []struct {
		name    string
		content Content
		want    string
		refused bool
	}{
		{"empty literal", Content{Base64: &empty}, "", false},
		{"literal", Content{Base64: &text}, "hi", false},
		{"cycle256", Content{Generator: "cycle256", Length: 3}, "\x00\x01\x02", false},
		{"both forms", Content{Base64: &text, Generator: "cycle256", Length: 3}, "", true},
		{"neither form", Content{}, "", true},
		{"unknown generator", Content{Generator: "zeros", Length: 3}, "", true},
		{"generated without length", Content{Generator: "cycle256"}, "", true},
		{"unpadded base64", Content{Base64: ptr("aGk")}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.content.Bytes()
			if c.refused {
				if err == nil {
					t.Fatalf("accepted, materialized %q", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Fatalf("materialized %q, want %q", got, c.want)
			}
		})
	}
}

func TestCycle256WrapsAtByteBoundary(t *testing.T) {
	b, err := Content{Generator: "cycle256", Length: 258}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if b[255] != 255 || b[256] != 0 || b[257] != 1 {
		t.Fatalf("bytes 255..257 are %v", b[255:258])
	}
}

func ptr(s string) *string { return &s }
