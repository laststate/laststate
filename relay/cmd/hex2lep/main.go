// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/hex"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: hex2lep <hex-string> <out-path>")
		os.Exit(2)
	}
	b, err := hex.DecodeString(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "decode:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[2], b, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", len(b), "bytes ->", os.Args[2])
}
