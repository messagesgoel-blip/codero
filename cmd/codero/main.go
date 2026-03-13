package main

import (
	"fmt"
	"os"
)

const version = "0.1.0-dev"

func usage() {
	fmt.Println("codero - code review orchestration control plane")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  codero <command>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  help       Show this help")
	fmt.Println("  version    Print version")
	fmt.Println("  status     Print scaffold status")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}

	switch os.Args[1] {
	case "help", "-h", "--help":
		usage()
	case "version":
		fmt.Println(version)
	case "status":
		fmt.Println("codero scaffold: ok")
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}
