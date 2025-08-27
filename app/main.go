package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
	case "clone":
		if len(os.Args) < 4 {
			fmt.Fprintf(os.Stderr, "usage: mygit clone <repo-url> <dir>\n")
			os.Exit(1)
		}
		repoUrl := os.Args[2]
		dir := os.Args[3]
		if err := clone(repoUrl, dir); err != nil {
			fmt.Fprintf(os.Stderr, "clone failed: %s\n", err)
			os.Exit(1)
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
	payload.WriteString("tree " + treeSha + "\n")

	// optional parent line
	if parentSha != "" {
		payload.WriteString("parent " + parentSha + "\n")
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

// ### CHANGE THIS ### - Clone function implementation
func clone(repoUrl, dir string) error {
	// Create target directory
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Change to target directory
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("failed to change to directory %s: %w", dir, err)
	}

	// Initialize git repository structure
	if err := initializeGitRepo(); err != nil {
		return fmt.Errorf("failed to initialize git repo: %w", err)
	}

	// Discover repository references
	refs, capabilities, err := discoverRefs(repoUrl)
	if err != nil {
		return fmt.Errorf("failed to discover refs: %w", err)
	}

	// ### CHANGE THIS ### - Debug: Print discovered refs
	fmt.Printf("Discovered %d refs:\n", len(refs))
	for _, ref := range refs {
		fmt.Printf("  %s -> %s\n", ref.Name, ref.Hash)
	}
	fmt.Printf("Capabilities: %v\n", capabilities)

	// Find the default branch (usually main or master)
	defaultRef := findDefaultRef(refs)
	if defaultRef == nil {
		return fmt.Errorf("no default branch found")
	}
	fmt.Printf("Using default ref: %s -> %s\n", defaultRef.Name, defaultRef.Hash)

	// Negotiate and receive packfile
	packData, err := negotiatePackfile(repoUrl, []*GitRef{defaultRef}, capabilities)
	if err != nil {
		return fmt.Errorf("failed to negotiate packfile: %w", err)
	}

	// Unpack the packfile
	if err := unpackPackfile(packData); err != nil {
		return fmt.Errorf("failed to unpack packfile: %w", err)
	}

	// Update references
	if err := updateRefs(refs, defaultRef); err != nil {
		return fmt.Errorf("failed to update refs: %w", err)
	}

	// Checkout the working tree
	if err := checkoutWorkingTree(defaultRef.Hash); err != nil {
		return fmt.Errorf("failed to checkout working tree: %w", err)
	}

	return nil
}

// GitRef represents a git reference
type GitRef struct {
	Name string
	Hash string
}

// initializeGitRepo creates the basic git repository structure
func initializeGitRepo() error {
	dirs := []string{".git", ".git/objects", ".git/refs", ".git/refs/heads", ".git/refs/remotes", ".git/refs/remotes/origin"}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

// discoverRefs discovers repository references using Git Smart HTTP protocol
func discoverRefs(repoUrl string) ([]*GitRef, []string, error) {
	// Parse URL and construct info/refs endpoint
	parsedUrl, err := url.Parse(repoUrl)
	if err != nil {
		return nil, nil, err
	}

	infoRefsUrl := parsedUrl.String()
	if !strings.HasSuffix(infoRefsUrl, ".git") {
		infoRefsUrl += ".git"
	}
	infoRefsUrl += "/info/refs?service=git-upload-pack"

	// Make HTTP request
	resp, err := http.Get(infoRefsUrl)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Parse the response
	return parseInfoRefsResponse(resp.Body)
}

// parseInfoRefsResponse parses the git-upload-pack info/refs response
func parseInfoRefsResponse(body io.Reader) ([]*GitRef, []string, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, nil, err
	}

	var refs []*GitRef
	var capabilities []string

	// ### CHANGE THIS ### - Debug: Print raw response
	fmt.Printf("Raw response length: %d\n", len(data))
	fmt.Printf("Raw response (first 500 chars): %q\n", string(data[:min(500, len(data))]))

	reader := bytes.NewReader(data)
	firstRefLine := true
	serviceAdvDone := false

	for reader.Len() > 0 {
		// Read pkt-line length
		lengthBytes := make([]byte, 4)
		if _, err := reader.Read(lengthBytes); err != nil {
			break
		}

		lengthStr := string(lengthBytes)
		length, err := strconv.ParseInt(lengthStr, 16, 64)
		if err != nil {
			fmt.Printf("Error parsing length %q: %v\n", lengthStr, err)
			break
		}

		if length == 0 {
			// Flush packet - if we haven't finished service advertisement, continue
			if !serviceAdvDone {
				serviceAdvDone = true
				fmt.Printf("Service advertisement done, continuing to refs\n")
				continue
			} else {
				// End of refs
				break
			}
		}

		if length < 4 {
			fmt.Printf("Invalid packet length: %d\n", length)
			break
		}

		// Read packet content
		contentLen := length - 4
		content := make([]byte, contentLen)
		if _, err := reader.Read(content); err != nil {
			fmt.Printf("Error reading content: %v\n", err)
			break
		}

		line := strings.TrimSuffix(string(content), "\n")
		fmt.Printf("Packet content: %q\n", line)

		// Skip service advertisement
		if strings.HasPrefix(line, "# service=") {
			continue
		}

		// Parse ref line - must contain a space and be a hash + ref
		if strings.Contains(line, " ") && len(line) > 40 {
			// Handle the null byte in the line for capabilities
			refLine := line
			if strings.Contains(line, "\x00") {
				// Extract capabilities from first ref line
				if firstRefLine {
					firstRefLine = false
					nullIdx := strings.Index(line, "\x00")
					if nullIdx != -1 && nullIdx < len(line)-1 {
						capStr := line[nullIdx+1:]
						capabilities = strings.Fields(capStr)
						fmt.Printf("Extracted capabilities: %v\n", capabilities)
					}
				}
				// Get the ref part (before null byte)
				refLine = line[:strings.Index(line, "\x00")]
			}

			parts := strings.Split(refLine, " ")
			if len(parts) >= 2 && len(parts[0]) == 40 {
				hash := parts[0]
				refName := parts[1]

				refs = append(refs, &GitRef{
					Name: refName,
					Hash: hash,
				})
				fmt.Printf("Added ref: %s -> %s\n", refName, hash)
			}
		}
	}

	return refs, capabilities, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// parsePktLine parses Git's packet line format
func parsePktLine(line string) ([]byte, error) {
	if len(line) < 4 {
		return nil, fmt.Errorf("line too short")
	}

	// Parse length prefix (4 hex chars)
	lengthStr := line[:4]
	length, err := strconv.ParseInt(lengthStr, 16, 64)
	if err != nil {
		return nil, err
	}

	if length == 0 {
		return []byte{}, nil // Flush packet
	}

	if int(length) > len(line) {
		return nil, fmt.Errorf("invalid packet length")
	}

	// Return the data part (minus the 4-byte length prefix)
	return []byte(line[4:length]), nil
}

// findDefaultRef finds the default branch (main, master, or first available)
func findDefaultRef(refs []*GitRef) *GitRef {
	// Look for main first
	for _, ref := range refs {
		if ref.Name == "refs/heads/main" {
			return ref
		}
	}

	// Then look for master
	for _, ref := range refs {
		if ref.Name == "refs/heads/master" {
			return ref
		}
	}

	// Return first head ref
	for _, ref := range refs {
		if strings.HasPrefix(ref.Name, "refs/heads/") {
			return ref
		}
	}

	return nil
}

// negotiatePackfile negotiates with the server to get the packfile
func negotiatePackfile(repoUrl string, wantRefs []*GitRef, capabilities []string) ([]byte, error) {
	// Parse URL and construct git-upload-pack endpoint
	parsedUrl, err := url.Parse(repoUrl)
	if err != nil {
		return nil, err
	}

	uploadPackUrl := parsedUrl.String()
	if !strings.HasSuffix(uploadPackUrl, ".git") {
		uploadPackUrl += ".git"
	}
	uploadPackUrl += "/git-upload-pack"

	// Construct request body
	var requestBody bytes.Buffer

	// Add wants
	for i, ref := range wantRefs {
		want := fmt.Sprintf("want %s", ref.Hash)
		if i == 0 {
			// Add capabilities to first want line
			want += " " + strings.Join([]string{"multi_ack_detailed", "side-band-64k", "thin-pack", "ofs-delta"}, " ")
		}
		want += "\n"

		pktLine := formatPktLine([]byte(want))
		requestBody.Write(pktLine)
	}

	// Add flush packet
	requestBody.Write([]byte("0000"))

	// Add done
	done := "done\n"
	pktLine := formatPktLine([]byte(done))
	requestBody.Write(pktLine)

	// Make POST request
	resp, err := http.Post(uploadPackUrl, "application/x-git-upload-pack-request", &requestBody)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Read and parse the response
	return parseUploadPackResponse(resp.Body)
}

// formatPktLine formats data as a Git packet line
func formatPktLine(data []byte) []byte {
	length := len(data) + 4
	lengthStr := fmt.Sprintf("%04x", length)
	return append([]byte(lengthStr), data...)
}

// parseUploadPackResponse parses the upload-pack response and extracts packfile data
func parseUploadPackResponse(body io.Reader) ([]byte, error) {
	var packData bytes.Buffer
	reader := bufio.NewReader(body)

	for {
		// Read packet length
		lengthBytes := make([]byte, 4)
		if _, err := io.ReadFull(reader, lengthBytes); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		lengthStr := string(lengthBytes)
		length, err := strconv.ParseInt(lengthStr, 16, 64)
		if err != nil {
			return nil, err
		}

		if length == 0 {
			continue // Flush packet
		}

		// Read packet data
		packetData := make([]byte, length-4)
		if _, err := io.ReadFull(reader, packetData); err != nil {
			return nil, err
		}

		// Check for side-band data
		if len(packetData) > 0 {
			sideband := packetData[0]
			data := packetData[1:]

			switch sideband {
			case 1: // Packfile data
				packData.Write(data)
			case 2: // Progress messages (ignore)
				continue
			case 3: // Error messages
				return nil, fmt.Errorf("server error: %s", string(data))
			}
		}
	}

	return packData.Bytes(), nil
}

// PackObject represents a parsed pack object
type PackObject struct {
	Type    int
	Size    int64
	Content []byte
	Hash    string
}

// unpackPackfile unpacks the received packfile and writes objects to .git/objects
func unpackPackfile(packData []byte) error {
	if len(packData) < 12 {
		return fmt.Errorf("packfile too short")
	}

	// ### CHANGE THIS ### - Debug: Print packfile info
	fmt.Printf("Packfile length: %d\n", len(packData))
	fmt.Printf("First 32 bytes: %q\n", string(packData[:min(32, len(packData))]))
	fmt.Printf("First 32 bytes (hex): %x\n", packData[:min(32, len(packData))])

	reader := bytes.NewReader(packData)

	// Verify pack signature
	signature := make([]byte, 4)
	if _, err := reader.Read(signature); err != nil {
		return err
	}
	fmt.Printf("Pack signature: %q\n", string(signature))
	if string(signature) != "PACK" {
		return fmt.Errorf("invalid pack signature: got %q", string(signature))
	}

	// Read version
	var version uint32
	if err := binary.Read(reader, binary.BigEndian, &version); err != nil {
		return err
	}
	if version != 2 {
		return fmt.Errorf("unsupported pack version: %d", version)
	}

	// Read number of objects
	var numObjects uint32
	if err := binary.Read(reader, binary.BigEndian, &numObjects); err != nil {
		return err
	}

	// Parse objects
	objects := make([]*PackObject, 0, numObjects)
	for i := uint32(0); i < numObjects; i++ {
		fmt.Printf("Parsing object %d/%d, remaining bytes: %d\n", i+1, numObjects, reader.Len())
		obj, err := parsePackObject(reader)
		if err != nil {
			return fmt.Errorf("failed to parse object %d: %w", i, err)
		}
		fmt.Printf("Object %d: type=%d, size=%d\n", i+1, obj.Type, obj.Size)
		objects = append(objects, obj)
	}

	// Resolve delta objects and write all objects
	return writePackObjects(objects)
}

// parsePackObject parses a single object from the packfile
func parsePackObject(reader *bytes.Reader) (*PackObject, error) {
	// Read type and size from variable-length encoding
	objType, size, err := readPackObjectHeader(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read header: %w", err)
	}

	fmt.Printf("  Object header: type=%d, size=%d\n", objType, size)

		// ### CHANGE THIS ### - Handle delta objects differently
	if objType == 6 || objType == 7 { // OFS_DELTA or REF_DELTA
		fmt.Printf("  Skipping delta object (type %d) for now\n", objType)
		// For delta objects, we need to read additional data first
		
		if objType == 6 { // OFS_DELTA - has offset
			// Read the offset (variable length)
			var offset int64
			for {
				b, err := reader.ReadByte()
				if err != nil {
					return nil, fmt.Errorf("failed to read delta offset: %w", err)
				}
				offset = (offset << 7) | int64(b&0x7f)
				if b&0x80 == 0 {
					break
				}
			}
			fmt.Printf("  Delta offset: %d\n", offset)
		} else if objType == 7 { // REF_DELTA - has 20-byte SHA
			sha := make([]byte, 20)
			if _, err := reader.Read(sha); err != nil {
				return nil, fmt.Errorf("failed to read delta sha: %w", err)
			}
			fmt.Printf("  Delta base SHA: %x\n", sha)
		}
		
		// Now read the compressed delta data
		zlibReader, err := zlib.NewReader(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to create zlib reader for delta: %w", err)
		}
		
		// Read and discard the delta data for now
		if _, err := io.Copy(io.Discard, zlibReader); err != nil {
			return nil, fmt.Errorf("failed to read delta data: %w", err)
		}
		zlibReader.Close()

		return &PackObject{
			Type:    objType,
			Size:    size,
			Content: nil, // Will be resolved later
		}, nil
	}

	// Read compressed data for regular objects
	zlibReader, err := zlib.NewReader(reader)
	if err != nil {
		// Print some context for debugging
		fmt.Printf("  Failed to create zlib reader at position, remaining bytes: %d\n", reader.Len())
		peek := make([]byte, min(32, reader.Len()))
		reader.Read(peek)
		reader.Seek(-int64(len(peek)), io.SeekCurrent) // Reset position
		fmt.Printf("  Next bytes: %x\n", peek)
		return nil, fmt.Errorf("failed to create zlib reader: %w", err)
	}

	var content bytes.Buffer
	if _, err := io.Copy(&content, zlibReader); err != nil {
		return nil, fmt.Errorf("failed to decompress: %w", err)
	}
	zlibReader.Close()

	return &PackObject{
		Type:    objType,
		Size:    size,
		Content: content.Bytes(),
	}, nil
}

// readPackObjectHeader reads the variable-length object header
func readPackObjectHeader(reader *bytes.Reader) (int, int64, error) {
	var b byte
	var err error
	var size int64
	var objType int

	// Read first byte
	if b, err = reader.ReadByte(); err != nil {
		return 0, 0, err
	}

	// Extract type and initial size
	objType = int((b >> 4) & 7)
	size = int64(b & 15)
	shift := uint(4)

	// Continue reading if MSB is set
	for b&0x80 != 0 {
		if b, err = reader.ReadByte(); err != nil {
			return 0, 0, err
		}
		size |= int64(b&0x7f) << shift
		shift += 7
	}

	return objType, size, nil
}

// writePackObjects writes pack objects to .git/objects, resolving deltas if needed
func writePackObjects(objects []*PackObject) error {
	// For simplicity, we'll assume no delta objects for now
	// In a full implementation, you'd need to resolve OFS_DELTA and REF_DELTA objects

	for _, obj := range objects {
		var objTypeStr string
		switch obj.Type {
		case 1: // commit
			objTypeStr = "commit"
		case 2: // tree
			objTypeStr = "tree"
		case 3: // blob
			objTypeStr = "blob"
		case 4: // tag
			objTypeStr = "tag"
		default:
			// Skip delta objects for now
			continue
		}

		// Write object to .git/objects
		hexSha, _, err := writeObject(objTypeStr, obj.Content)
		if err != nil {
			return fmt.Errorf("failed to write %s object: %w", objTypeStr, err)
		}
		obj.Hash = hexSha
	}

	return nil
}

// updateRefs updates local references to match the remote
func updateRefs(refs []*GitRef, defaultRef *GitRef) error {
	// Write remote refs
	for _, ref := range refs {
		if strings.HasPrefix(ref.Name, "refs/heads/") {
			// Create corresponding remote tracking branch
			branchName := strings.TrimPrefix(ref.Name, "refs/heads/")
			remotePath := filepath.Join(".git", "refs", "remotes", "origin", branchName)

			if err := os.MkdirAll(filepath.Dir(remotePath), 0755); err != nil {
				return err
			}

			if err := os.WriteFile(remotePath, []byte(ref.Hash+"\n"), 0644); err != nil {
				return err
			}

			// If this is the default branch, also create the local branch
			if ref.Hash == defaultRef.Hash {
				localPath := filepath.Join(".git", "refs", "heads", branchName)
				if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
					return err
				}
				if err := os.WriteFile(localPath, []byte(ref.Hash+"\n"), 0644); err != nil {
					return err
				}
			}
		}
	}

	// Set HEAD to point to the default branch
	defaultBranch := strings.TrimPrefix(defaultRef.Name, "refs/heads/")
	headContent := fmt.Sprintf("ref: refs/heads/%s\n", defaultBranch)
	return os.WriteFile(".git/HEAD", []byte(headContent), 0644)
}

// checkoutWorkingTree checks out files from the commit to the working directory
func checkoutWorkingTree(commitHash string) error {
	// Read commit object to get tree hash
	commitContent, err := readObject(commitHash)
	if err != nil {
		return fmt.Errorf("failed to read commit %s: %w", commitHash, err)
	}

	// Parse commit to find tree hash
	lines := strings.Split(string(commitContent), "\n")
	var treeHash string
	for _, line := range lines {
		if strings.HasPrefix(line, "tree ") {
			treeHash = strings.TrimPrefix(line, "tree ")
			break
		}
	}

	if treeHash == "" {
		return fmt.Errorf("no tree found in commit")
	}

	// Recursively checkout the tree
	return checkoutTree(treeHash, ".")
}

// checkoutTree recursively checks out a tree object to the filesystem
func checkoutTree(treeHash string, basePath string) error {
	payload, err := readObject(treeHash)
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
			if err := checkoutTree(shaHex, fullPath); err != nil {
				return err
			}
		} else {
			// File - read blob and write to filesystem
			content, err := readObject(shaHex)
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
