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
	"sort"
	"strconv"
	"time"
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
	case "write-tree":
		sha, err := writeTree(".")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing tree: %s\n", err)
			os.Exit(1)
		}
		fmt.Println(sha)
	case "commit-tree":
		if len(os.Args) < 7 {
			fmt.Fprintf(os.Stderr, "usage: mygit commit-tree <tree_sha> -p <parent_sha> -m <message> \n")
			os.Exit(1)
		}
		treeSha := os.Args[2]
		parentSha := os.Args[4]
		message := os.Args[6]
		commitTree(treeSha, parentSha, message)
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

// writeObject writes an object of type objType ("blob" or "tree") with the provided
// content (already the raw content for the object, e.g. blob bytes or tree payload).
// It returns the 40-char hex SHA, the raw 20-byte SHA, or an error.
func writeObject(objType string, content []byte) (string, [20]byte, error) {
	header := objType + " " + strconv.Itoa(len(content)) + "\x00"

	hasher := sha1.New()
	_, _ = hasher.Write([]byte(header))
	_, _ = hasher.Write(content)
	sum := hasher.Sum(nil)

	var raw [20]byte
	copy(raw[:], sum)
	hexStr := hex.EncodeToString(sum)

	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write([]byte(header)); err != nil {
		return "", raw, err
	}
	if _, err := w.Write(content); err != nil {
		return "", raw, err
	}
	if err := w.Close(); err != nil {
		return "", raw, err
	}
	objectsDir := filepath.Join(".git", "objects", hexStr[:2])
	if err := os.MkdirAll(objectsDir, 0755); err != nil {
		return "", raw, err
	}

	// fixed: write the file under the remaining hex chars (not the prefix again)
	objectPath := filepath.Join(objectsDir, hexStr[2:])
	if _, err := os.Stat(objectPath); os.IsNotExist(err) {
		if err := os.WriteFile(objectPath, buf.Bytes(), 0644); err != nil {
			return "", raw, err
		}
	}
	return hexStr, raw, nil
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

	// write compressed object only when -w was passed, reuse writeObject
	if write {
		if _, _, err := writeObject("blob", data); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing blob object: %s\n", err)
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

// writeTree builds a tree object for the directory 'dir' (recursively),
// writes any needed blob/tree objects to .git/objects and returns the
// 40-char hex SHA of the created tree.
func writeTree(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	var names []string
	for _, e := range entries {
		if e.Name() == ".git" {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	var payload bytes.Buffer

	for _, name := range names {
		full := filepath.Join(dir, name)
		info, err := os.Lstat(full)
		if err != nil {
			return "", err
		}

		var mode string
		var shaRaw [20]byte

		if info.IsDir() {
			// create subtree and get its hex SHA
			subHex, err := writeTree(full)
			if err != nil {
				return "", err
			}
			shaBytes, err := hex.DecodeString(subHex)
			if err != nil {
				return "", err
			}
			copy(shaRaw[:], shaBytes)
			mode = "40000"
		} else if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(full)
			if err != nil {
				return "", err
			}
			_, raw, err := writeObject("blob", []byte(linkTarget))
			if err != nil {
				return "", err
			}
			shaRaw = raw
			mode = "120000"
		} else {
			// regular file -> create blob object
			data, err := os.ReadFile(full)
			if err != nil {
				return "", err
			}
			_, raw, err := writeObject("blob", data)
			if err != nil {
				return "", err
			}
			shaRaw = raw

			// determine file mode (executable or not)
			if info.Mode()&0111 != 0 {
				mode = "100755"
			} else {
				mode = "100644"
			}
		}

		// append entry: "<mode> <name>\x00<20_byte_sha>"
		payload.WriteString(mode)
		payload.WriteByte(' ')
		payload.WriteString(name)
		payload.WriteByte(0)
		payload.Write(shaRaw[:])
	}

	// write tree object using writeObject helper
	treeHex, _, err := writeObject("tree", payload.Bytes())
	if err != nil {
		return "", err
	}
	return treeHex, nil
}

func commitTree(treeSha string, parentSha string, message string) {
	var payload bytes.Buffer

	// tree line
	payload.WriteString("tree" + treeSha + "\n")

	// optional parent line
	if parentSha != "" {
		payload.WriteString("parent" + parentSha + "\n")
	}

	// author / committer (hardcoded name/email allowed)
	now := time.Now()
	ts := now.Unix()
	_, offset := now.Zone()
	sign := '+'
	if offset < 0 {
		sign = '-'
		offset = -offset
	}
	h := offset / 3600
	m := (offset % 3600) / 60
	tz := fmt.Sprintf("%c%02d%02d", sign, h, m)

	author := "Ronit Rameja <ronitrameja28@gmail.com>"
	payload.WriteString(fmt.Sprintf("author %s %d %s\n", author, ts, tz))
	payload.WriteString(fmt.Sprintf("committer %s %d %s\n", author, ts, tz))

	// blank line then commit message
	payload.WriteByte('\n')
	payload.WriteString(message)
	payload.WriteByte('\n')

	// write commit object
	commitHex, _, err := writeObject("commit", payload.Bytes())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing commit object: %s\n", err)
		return
	}
	fmt.Println(commitHex)
}
