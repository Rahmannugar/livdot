package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Rahmannugar/livdot/internal/openapi"
)

const outputPath = "openapi.json"

func main() {
	document := openapi.Document()
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode openapi document:", err)
		os.Exit(1)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(outputPath, encoded, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write openapi document:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", outputPath)
}
