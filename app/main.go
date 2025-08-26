package main

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"strings"
)

// Usage: your_program.sh <command> <arg1> <arg2> ...
func main() {

	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: mygit <command> [<args>...]\n")
		os.Exit(1)
	}

	switch command := os.Args[1]; command {
	case "init":
		for _, dir := range []string{".git", ".git/objects", ".git/refs"} {
			if err := os.MkdirAll(dir, 0755); err != nil {
				fmt.Fprintf(os.Stderr, "Error creating directory: %s\n", err)
			}
		}

		headFileContents := []byte("ref: refs/heads/main\n")
		if err := os.WriteFile(".git/HEAD", headFileContents, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing file: %s\n", err)
		}

		fmt.Println("Initialized git directory")
	case "cat-file":
		hash := os.Args[3]
		data, err := os.ReadFile(".git/objects/" + hash[:2] + "/" + hash[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading file: %s\n", err)
			return
		}
		r, err := zlib.NewReader(bytes.NewReader(data))
		if err != nil {
			fmt.Printf("Error: %s", err.Error())
			return
		}
		var out bytes.Buffer
		io.Copy(&out, r)
		content := out.String()
		if i := strings.IndexByte(content, '\x00'); i != -1 {
			content = content[i+1:]
		}
		fmt.Print(content)
		r.Close()

	default:
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", command)
		os.Exit(1)
	}
}
