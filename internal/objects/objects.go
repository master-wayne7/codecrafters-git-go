package objects

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
	"time"
)

// ReadObject reads a Git object from the objects directory
func ReadObject(hash string) ([]byte, error) {
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

// WriteObject writes an object of type objType ("blob", "tree", "commit", "tag") with the provided
// content (already the raw content for the object, e.g. blob bytes or tree payload).
// It returns the 40-char hex SHA, the raw 20-byte SHA, or an error.
func WriteObject(objType string, content []byte) (string, [20]byte, error) {
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

// HashObject computes the hash for a file and optionally writes it to objects
func HashObject(file string, write bool) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("error reading file: %w", err)
	}

	// create header that must be included in the SHA computation
	header := "blob " + strconv.Itoa(len(data)) + "\x00"

	// compute SHA over header + data (before compression)
	hasher := sha1.New()
	_, _ = hasher.Write([]byte(header))
	_, _ = hasher.Write(data)
	sha1HashBytes := hasher.Sum(nil)
	sha1HexString := hex.EncodeToString(sha1HashBytes)

	// write compressed object only when -w was passed, reuse WriteObject
	if write {
		if _, _, err := WriteObject("blob", data); err != nil {
			return "", fmt.Errorf("error writing blob object: %w", err)
		}
	}

	return sha1HexString, nil
}

// CatFile prints the content of a Git object
func CatFile(hash string) error {
	content, err := ReadObject(hash)
	if err != nil {
		return err
	}
	fmt.Print(string(content))
	return nil
}

// TreeEntry represents an entry in a Git tree
type TreeEntry struct {
	Mode string
	Name string
	Hash string
	Type string
}

// ParseTree parses a tree object and returns its entries
func ParseTree(hash string) ([]TreeEntry, error) {
	payload, err := ReadObject(hash)
	if err != nil {
		return nil, fmt.Errorf("error reading tree: %w", err)
	}

	var entries []TreeEntry
	cursor := 0

	for cursor < len(payload) {
		// parse mode (bytes until space)
		spaceIdx := bytes.IndexByte(payload[cursor:], ' ')
		if spaceIdx == -1 {
			break
		}

		modeBytes := payload[cursor : cursor+spaceIdx]
		modeStr := string(modeBytes)
		cursor += spaceIdx + 1 // move ahead of space

		nullIdx := bytes.IndexByte(payload[cursor:], '\x00')
		if nullIdx == -1 {
			break
		}

		name := string(payload[cursor : cursor+nullIdx])
		cursor += nullIdx + 1 // move ahead of null byte

		// reading 20 bytes raw sha of tree
		if len(payload)-cursor < 20 {
			break
		}

		shaRaw := payload[cursor : cursor+20]
		shaHex := hex.EncodeToString(shaRaw)
		cursor += 20

		objType := "blob"
		modeOut := modeStr
		if modeStr == "40000" {
			objType = "tree"
			// Git typically prints tree mode as 040000
			modeOut = "040000"
		}

		entries = append(entries, TreeEntry{
			Mode: modeOut,
			Name: name,
			Hash: shaHex,
			Type: objType,
		})
	}

	return entries, nil
}

// LsTree lists the contents of a tree object
func LsTree(hash string, nameOnly bool) error {
	entries, err := ParseTree(hash)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if nameOnly {
			fmt.Println(entry.Name)
		} else {
			fmt.Printf("%s %s %s\t%s\n", entry.Mode, entry.Type, entry.Hash, entry.Name)
		}
	}

	return nil
}

// WriteTreeEntry represents an entry for building a tree
type WriteTreeEntry struct {
	Mode   string
	Name   string
	ShaRaw [20]byte
	IsTree bool
}

// WriteTree builds a tree object for the directory 'dir' (recursively),
// writes any needed blob/tree objects to .git/objects and returns the
// 40-char hex SHA of the created tree.
func WriteTree(dir string) (string, error) {
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

	// Sort names for consistent tree ordering
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[i] > names[j] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}

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
			subHex, err := WriteTree(full)
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
			_, raw, err := WriteObject("blob", []byte(linkTarget))
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
			_, raw, err := WriteObject("blob", data)
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

	// write tree object using WriteObject helper
	treeHex, _, err := WriteObject("tree", payload.Bytes())
	if err != nil {
		return "", err
	}
	return treeHex, nil
}

// CommitTree creates a commit object
func CommitTree(treeSha string, parentSha string, message string) (string, error) {
	var payload bytes.Buffer

	// tree line
	payload.WriteString("tree " + treeSha + "\n")

	// optional parent line
	if parentSha != "" {
		payload.WriteString("parent " + parentSha + "\n")
	}

	// author / committer (hardcoded name/email allowed)
	// Import time package for this
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
	commitHex, _, err := WriteObject("commit", payload.Bytes())
	if err != nil {
		return "", fmt.Errorf("error writing commit object: %w", err)
	}
	return commitHex, nil
}

// CheckoutTree recursively checks out a tree object to the filesystem
func CheckoutTree(treeHash string, basePath string) error {
	payload, err := ReadObject(treeHash)
	if err != nil {
		// ### CHANGE THIS ### - Skip missing objects (likely deltas that weren't resolved)
		fmt.Printf("Warning: Skipping missing object %s in path %s\n", treeHash, basePath)
		return nil
	}

	cursor := 0
	for cursor < len(payload) {
		// Parse mode
		spaceIdx := bytes.IndexByte(payload[cursor:], ' ')
		if spaceIdx == -1 {
			break
		}
		modeBytes := payload[cursor : cursor+spaceIdx]
		mode := string(modeBytes)
		cursor += spaceIdx + 1

		// Parse name
		nullIdx := bytes.IndexByte(payload[cursor:], '\x00')
		if nullIdx == -1 {
			break
		}
		name := string(payload[cursor : cursor+nullIdx])
		cursor += nullIdx + 1

		// Parse hash
		if len(payload)-cursor < 20 {
			break
		}
		shaRaw := payload[cursor : cursor+20]
		shaHex := hex.EncodeToString(shaRaw)
		cursor += 20

		// Create the file/directory
		fullPath := filepath.Join(basePath, name)

		if mode == "40000" {
			// Directory - create it and recurse
			if err := os.MkdirAll(fullPath, 0755); err != nil {
				return err
			}
			if err := CheckoutTree(shaHex, fullPath); err != nil {
				return err
			}
		} else {
			// File - read blob and write to filesystem
			content, err := ReadObject(shaHex)
			if err != nil {
				// ### CHANGE THIS ### - Skip missing blob objects
				fmt.Printf("Warning: Skipping missing blob %s for file %s\n", shaHex, fullPath)
				continue
			}

			// Determine file permissions
			var perm os.FileMode = 0644
			if mode == "100755" {
				perm = 0755
			}

			if err := os.WriteFile(fullPath, content, perm); err != nil {
				return err
			}
		}
	}

	return nil
}
