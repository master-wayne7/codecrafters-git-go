package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
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
		initFunction()
	case "cat-file":
		catFile()
	case "hash-object":
		hashObject()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", command)
		os.Exit(1)
	}
}

func initFunction() {
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
}
func catFile() {
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
}

func hashObject() {
	file := os.Args[3]
	data, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %s\n", err)
		return
	}

	// create header that must be included in the SHA computation
	header := "blob " + strconv.Itoa(len(data)) + "\x00"

	// compute SHA over header + data (before compression)
	hasher := sha1.New()
	hasher.Write([]byte(header))
	hasher.Write(data)
	sha1HashBytes := hasher.Sum(nil)
	sha1HexString := hex.EncodeToString(sha1HashBytes)

	// compress the same header + data and write to object file
	var b bytes.Buffer
	w := zlib.NewWriter(&b)

	if _, err := w.Write([]byte(header)); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing header to zlib writer: %s\n", err)
		return
	}
	if _, err := w.Write(data); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing content to zlib writer: %s\n", err)
		return
	}

	// Close to flush compressed data into the buffer
	if err := w.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Error closing zlib writer: %s\n", err)
		return
	}

	objectsDir := ".git/objects/" + sha1HexString[:2]
	if err := os.MkdirAll(objectsDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating directory: %s\n", err)
		return
	}

	objectPath := filepath.Join(objectsDir, sha1HexString[2:])
	if err := os.WriteFile(objectPath, b.Bytes(), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing object file: %s\n", err)
		return
	}

	fmt.Println(sha1HexString)
}
