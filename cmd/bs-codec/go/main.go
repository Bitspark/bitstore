// bs-codec exchanges JSON on stdin/stdout for native codec interchange checks.
// A request supplies either flat (hex) or root and chunks (name -> hex).
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	bs "github.com/Bitspark/bitstore/store/go"
)

type artifact struct {
	Flat   string             `json:"flat"`
	Root   bs.Root            `json:"root"`
	Chunks map[bs.Name]string `json:"chunks"`
}

func run() error {
	var in artifact
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		return err
	}
	ctx := context.Background()
	var d *bs.Data
	if in.Flat != "" {
		b, err := hex.DecodeString(in.Flat)
		if err != nil {
			return err
		}
		d, err = bs.DecodeFlat(b, bs.DataLimits{})
		if err != nil {
			return err
		}
	} else {
		s := bs.NewMemory(32 << 20)
		for n, h := range in.Chunks {
			b, err := hex.DecodeString(h)
			if err != nil {
				return err
			}
			got, err := s.Put(ctx, b)
			if err != nil {
				return err
			}
			if got != n {
				return bs.ErrIntegrity
			}
		}
		var err error
		d, err = bs.LoadData(ctx, s, in.Root, bs.DataLimits{})
		if err != nil {
			return err
		}
	}
	b, err := bs.EncodeFlat(d, bs.DataLimits{})
	if err != nil {
		return err
	}
	root, chunks, err := bs.EncodeLinked(d, bs.DataLimits{})
	if err != nil {
		return err
	}
	out := artifact{Flat: hex.EncodeToString(b), Root: root, Chunks: map[bs.Name]string{}}
	for n, b := range chunks {
		out.Chunks[n] = hex.EncodeToString(b)
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
