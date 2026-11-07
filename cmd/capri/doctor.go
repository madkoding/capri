package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"capri/internal/doctor"
)

// runDoctor implementa `capri doctor [--json]`.
func runDoctor(args []string) {
	asJSON := false
	for _, a := range args {
		switch a {
		case "--json":
			asJSON = true
		case "-h", "--help":
			fmt.Println("uso: capri doctor [--json]")
			return
		}
	}
	rep := doctor.Run(context.Background())
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
	} else {
		rep.Print()
		fmt.Println("\n" + rep.Summary())
	}
	if rep.Failed() {
		os.Exit(1)
	}
}
