// Package conformance holds bitstore's language-independent conformance
// material: the naming vectors every store, server and SDK backend must
// reproduce. See README.md for the vector format and the behavioral
// checklist the vectors cannot carry.
package conformance

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

//go:embed vectors.json
var vectorsJSON []byte

// A Vector is content in, name and size out.
type Vector struct {
	Comment string  `json:"comment"`
	Content Content `json:"content"`
	Name    string  `json:"name"`
	Size    int64   `json:"size"`
}

// Content is exactly one of two forms: literal base64, or a generator with
// a length. Base64 is a pointer because the empty blob is literal content
// whose encoding is the empty string.
type Content struct {
	Base64    *string `json:"base64,omitempty"`
	Generator string  `json:"generator,omitempty"`
	Length    int64   `json:"length,omitempty"`
}

// Vectors decodes the committed vectors, refusing unknown members and
// content that is not exactly one of the two forms.
func Vectors() ([]Vector, error) {
	var file struct {
		Vectors []Vector `json:"vectors"`
	}
	dec := json.NewDecoder(bytes.NewReader(vectorsJSON))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("vectors.json: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("vectors.json: trailing data after the document")
	}
	for i, v := range file.Vectors {
		if err := v.Content.check(); err != nil {
			return nil, fmt.Errorf("vectors.json: vector %d (%s): %w", i, v.Comment, err)
		}
	}
	return file.Vectors, nil
}

// Bytes materializes the content a vector denotes.
func (c Content) Bytes() ([]byte, error) {
	if err := c.check(); err != nil {
		return nil, err
	}
	if c.Base64 != nil {
		return base64.StdEncoding.Strict().DecodeString(*c.Base64)
	}
	b := make([]byte, c.Length)
	for i := range b {
		b[i] = byte(i % 256)
	}
	return b, nil
}

func (c Content) check() error {
	switch {
	case c.Base64 != nil && (c.Generator != "" || c.Length != 0):
		return fmt.Errorf("content is both literal and generated")
	case c.Base64 != nil:
		return nil
	case c.Generator != "cycle256":
		return fmt.Errorf("unknown generator %q", c.Generator)
	case c.Length <= 0:
		return fmt.Errorf("generated content needs a positive length, got %d", c.Length)
	}
	return nil
}
