package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
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

		if os.Args[2] != "-p" || len(os.Args) < 4 {
			fmt.Fprintf(os.Stderr, "usage: mygit cat-file -p <hash>\n")
			os.Exit(1)
		}
		catFile(os.Args[3])
	case "hash-object":

		if os.Args[2] == "-w" {
			if len(os.Args) < 4 {
				fmt.Fprintf(os.Stderr, "usage: mygit hash-object -w <file>\n")
				os.Exit(1)
			}
			hashObject(os.Args[3], true)
		} else {
			hashObject(os.Args[2], false)
		}
	case "ls-tree":

		if os.Args[2] == "--name-only" {
			if len(os.Args) < 4 {
				fmt.Fprintf(os.Stderr, "usage: mygit ls-tree --name-only <tree_hash>\n")
				os.Exit(1)
			}
			lsTree(os.Args[3], true)
		} else {
			lsTree(os.Args[2], false)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", command)
		os.Exit(1)
	}
}

// init command
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

func readObject(hash string) ([]byte, error) {
	data, err := os.ReadFile(".git/objects/" + hash[:2] + "/" + hash[2:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %s\n", err)
		return nil, err
	}
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		fmt.Printf("Error: %s", err.Error())
		return nil, err
	}
	var out bytes.Buffer
	io.Copy(&out, r)
	r.Close()
	outBytes := out.Bytes()
	if i := bytes.IndexByte(outBytes, '\x00'); i != -1 {
		return outBytes[i+1:], nil
	}
	return nil, errors.New("file empty")
}

// cat-file command
func catFile(hash string) {

	content, err := readObject(hash)
	if err != nil {
		fmt.Print(err.Error())
	}

	fmt.Print(string(content))
}

// hash-object command
func hashObject(file string, write bool) {
	data, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %s\n", err)
		return
	}

	// create header that must be included in the SHA computation
	header := "blob " + strconv.Itoa(len(data)) + "\x00"

	// compute SHA over header + data (before compression)
	hasher := sha1.New()
	_, _ = hasher.Write([]byte(header))
	_, _ = hasher.Write(data)
	sha1HashBytes := hasher.Sum(nil)
	sha1HexString := hex.EncodeToString(sha1HashBytes)

	// write compressed object only when -w was passed
	if write {
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
	}

	fmt.Println(sha1HexString)
}

// ls-tree command
func lsTree(hash string, nameOnly bool) {
	payload, err := readObject(hash)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading tree")
		return
	}

	// keep track of index
	cursor := 0

	for cursor < len(payload) {
		// parse mode (bytes until space)
		spaceIdx := bytes.IndexByte(payload[cursor:], ' ')
		if spaceIdx == -1 {
			fmt.Fprintf(os.Stderr, "Malformed tree")
			return
		}

		modeBytes := payload[cursor : cursor+spaceIdx]
		modeStr := string(modeBytes)
		cursor += spaceIdx + 1 // move ahead of space

		nullIdx := bytes.IndexByte(payload[cursor:], '\x00')
		if nullIdx == -1 {
			fmt.Fprintf(os.Stderr, "Malformed tree")
			return
		}

		name := string(payload[cursor : cursor+nullIdx])
		cursor += nullIdx + 1 // move ahead of null byte

		// reading 20 bytes raw sha of tree
		if len(payload)-cursor < 20 {
			fmt.Fprintf(os.Stderr, "Malformed tree")
			return
		}

		shaRaw := payload[cursor : cursor+20]
		shaHex := hex.EncodeToString(shaRaw)
		cursor += 20

		// output
		if nameOnly {
			fmt.Println(name)
		} else {
			objType := "blob"
			modeOut := modeStr
			if modeStr == "40000" {
				objType = "tree"
				// Git typically prints tree mode as 040000
				modeOut = "040000"
			}
			fmt.Printf("%s %s %s\t%s\n", modeOut, objType, shaHex, name)
		}
	}
}
