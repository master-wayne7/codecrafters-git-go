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

// keep arrays of reconstructed objects for ofs-delta base lookup
type objEntry struct {
	typ    int
	data   []byte
	sha    string
	offset int64 // track pack offset for ofs-delta lookup
}

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

// clone implements a minimal smart-HTTP client that:
//   - discovers refs via /info/refs?service=git-upload-pack
//   - requests the target branch commit via git-upload-pack POST
//   - parses pkt-line / side-band responses and extracts the packfile
//   - parses the packfile, reconstructs objects (including deltas) and writes them to .git/objects
//   - writes refs/heads/<branch> and HEAD and checks out files into the working tree
func clone(repoUrl string, dir string) error {
	// creating the target dir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// normalize the url
	u, err := url.Parse(repoUrl)
	if err != nil {
		return err
	}

	// ensure path is ending with .git
	if !strings.HasSuffix(u.Path, ".git") {
		u.Path = strings.TrimSuffix(u.Path, "/") + ".git"
	}
	base := u.String()

	// fetch advertised refs
	refs, err := fetchInfoRefs(base)
	if err != nil {
		return err
	}

	// chose a branch to checkout
	branchRef, branchSha := choseBranchRef(refs)
	if branchRef == "" {
		return errors.New("no branch ref found")
	}

	// request pack containing branch sha
	pack, err := fetchPack(base, branchSha)
	if err != nil {
		return err
	}

	// prepare .git directory inside target dir
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "objects"), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(gitDir, "refs", "heads"), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(gitDir, "refs", "remotes", "origin"), 0755); err != nil {
		return err
	}

	// parse pack and write objects into .git/objects
	if err := parsePackAndWrite(pack, gitDir); err != nil {
		return err
	}

	// write branch ref and HEAD
	if err := os.WriteFile(filepath.Join(gitDir, "refs", "heads", filepath.Base(branchRef)), []byte(branchSha+"\n"), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/"+filepath.Base(branchRef)+"\n"), 0644); err != nil {
		return err
	}

	// checkout working tree from commit
	if err := checkoutCommitIntoDir(branchSha, dir, gitDir); err != nil {
		return err
	}

	return nil
}

// fetchInfoRefs GET /info/refs?service=git-upload-pack and returns map ref->sha (hex)
func fetchInfoRefs(base string) (map[string]string, error) {
	u := strings.TrimSuffix(base, "/") + "/info/refs?service=git-upload-pack"

	client := &http.Client{Timeout: 12 * time.Second}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	// important headers for git smart-http
	req.Header.Set("Accept", "application/x-git-upload-pack-advertisement")
	req.Header.Set("User-Agent", "git/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet := make([]byte, 512)
		n, _ := resp.Body.Read(snippet)
		return nil, fmt.Errorf("info/refs returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet[:n])))
	}

	// peek first bytes for easier debugging if server returns HTML
	br := bufio.NewReader(resp.Body)
	peek, _ := br.Peek(8)
	if len(peek) > 0 {
		// if it looks like "<!DOCTYPE" or "<html", fail fast with helpful message
		s := strings.ToLower(string(peek))
		if strings.Contains(s, "<!doctype") || strings.Contains(s, "<html") || strings.HasPrefix(s, "<!") {
			snip := make([]byte, 1024)
			n, _ := br.Read(snip)
			return nil, fmt.Errorf("unexpected HTML response from server: %s", strings.TrimSpace(string(snip[:n])))
		}
	}

	r := bufio.NewReader(br)
	refs := map[string]string{}
	for {
		line, err := readPktLine(r)
		if err != nil {
			return nil, err
		}
		if line == nil { // flush
			break
		}
		parts := bytes.Split(line, []byte{'\n'})
		for _, p := range parts {
			if len(p) == 0 {
				continue
			}
			nul := bytes.IndexByte(p, 0)
			if nul != -1 {
				head := p[:nul]
				fields := bytes.SplitN(head, []byte{' '}, 2)
				if len(fields) == 2 {
					sha := string(fields[0])
					ref := string(fields[1])
					refs[ref] = sha
				}
			} else {
				fields := bytes.SplitN(p, []byte{' '}, 2)
				if len(fields) == 2 {
					sha := string(fields[0])
					ref := string(fields[1])
					refs[ref] = sha
				}
			}
		}
	}
	return refs, nil
}

// choseBranchRef selects a branch ref name and sha; prefer refs/heads/main then any ref/heads/*
func choseBranchRef(refs map[string]string) (string, string) {
	if sha, ok := refs["refs/heads/main"]; ok {
		return "refs/heads/main", sha
	}
	for r, s := range refs {
		if strings.HasPrefix(r, "refs/heads/") {
			return r, s
		}
	}

	// fallback pick any ref
	for r, s := range refs {
		return r, s
	}
	return "", ""
}

// build an upload pack request asking for one want and declaring side band  64k compability
func buildUploadPackRequest(wantSha string) []byte {
	var b bytes.Buffer
	writePkt(&b, fmt.Sprintf("want %s side-band-64k thin-pack\n", wantSha))
	writePkt(&b, "done\n")
	// trailing flush is not required strictly
	b.WriteString("0000")
	return b.Bytes()
}

func writePkt(w io.Writer, s string) {
	// length = 4 + payload
	l := 4 + len(s)
	fmt.Fprintf(w, "%04x", l)
	w.Write([]byte(s))
}

// fetchPack posts upload-pack request and returns raw packfile bytes
func fetchPack(base string, wantSha string) ([]byte, error) {
	u := strings.TrimSuffix(base, "/") + "/git-upload-pack"
	reqbody := buildUploadPackRequest(wantSha)

	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest("POST", u, bytes.NewReader(reqbody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	req.Header.Set("Accept", "application/x-git-upload-pack-result")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet := make([]byte, 512)
		n, _ := resp.Body.Read(snippet)
		return nil, fmt.Errorf("git-upload-pack returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet[:n])))
	}

	reader := bufio.NewReader(resp.Body)
	var packBuf bytes.Buffer

	for {
		line, err := readPktLine(reader)
		if err != nil {
			return nil, err
		}
		if line == nil {
			continue
		}
		if len(line) == 0 {
			continue
		}
		// side band detection first byte is channel 1,2,3
		if line[0] == '1' || line[0] == '2' || line[0] == '3' {
			ch := line[0]
			payload := line[1:]
			switch ch {
			case '1':
				// data
				packBuf.Write(payload)
			case '2':
				// progress: write to stderr
				fmt.Fprintf(os.Stderr, "%s", payload)
			case '3':
				// error
				return nil, fmt.Errorf("remote error: %s", string(payload))
			}
			continue
		}
		// if line starts with PACK then server switched to raw pack
		if bytes.HasPrefix(line, []byte("PACK")) {
			// write this line and then copy the rest of the response
			packBuf.Write(line)
			_, err := io.Copy(&packBuf, reader)
			if err != nil {
				return nil, err
			}
			break
		}
		// otherwise packet line could contain textual status
	}
	return packBuf.Bytes(), nil
}

func readPktLine(r *bufio.Reader) ([]byte, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(r, hdr); err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, err
	}

	// validate header contains hex digits
	for _, b := range hdr {
		if !((b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')) {
			return nil, fmt.Errorf("invalid pkt-line header %q", string(hdr))
		}
	}

	l, err := strconv.ParseInt(string(hdr), 16, 0)
	if err != nil {
		return nil, err
	}
	if l == 0 {
		return nil, nil // flush packet
	}
	payloadLen := int(l) - 4
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// parsePackAndWrite parses pack[] and writes objects into gitDir/.git/objects via writeObject
func parsePackAndWrite(pack []byte, gitDir string) error {
	if len(pack) < 12 {
		return errors.New("pack too small")
	}
	if string(pack[0:4]) != "PACK" {
		return errors.New("invalid pack header")
	}
	version := binary.BigEndian.Uint32(pack[4:8])
	if version != 2 && version != 3 {
		return fmt.Errorf("unsupported pack version %d", version)
	}
	count := binary.BigEndian.Uint32(pack[8:12])
	offset := 12

	objects := make([]objEntry, 0, count)
	for i := uint32(0); i < count; i++ {
		if offset >= len(pack) {
			return errors.New("unexpected end of pack")
		}
		objectStart := int64(offset) // track starting offset for this object
		// parse object header (type + size) variable length
		first := pack[offset]
		offset++
		objType := int((first >> 4) & 0x7)
		size := int(first & 0x0f)
		shift := 4
		for (first & 0x80) != 0 {
			first = pack[offset]
			offset++
			size |= int(first&0x7f) << shift
			shift += 7
		}
		// handle delta headers
		var baseSha string
		var baseOffset int64
		switch objType {
		case 6: // ofs-delta
			// offset is encoded as variable-length big-endian base offset
			// see git pack format: a variable-length offset where MSB set indicates continuation
			// compute base offset backward from current offset
			n := 0
			c := pack[offset]
			offset++
			baseOffset = int64(c & 0x7f)
			for (c & 0x80) != 0 {
				c = pack[offset]
				offset++
				baseOffset = (baseOffset + 1) << 7
				baseOffset |= int64(c & 0x7f)
				n++
			}
		case 7: // ref-delta
			// next 20 bytes are base object's sha1
			if offset+20 > len(pack) {
				return errors.New("pack truncated in ref-delta")
			}
			baseSha = hex.EncodeToString(pack[offset : offset+20])
			offset += 20
		}

		// now compressed data: create a reader over pack[offset:]
		inner := bytes.NewReader(pack[offset:])
		zr, err := zlib.NewReader(inner)
		if err != nil {
			return fmt.Errorf("zlib new reader failed: %w", err)
		}
		// read decompressed bytes (size may be target size for non-delta; for delta it's delta size)
		var decompressed bytes.Buffer
		if _, err := io.Copy(&decompressed, zr); err != nil {
			zr.Close()
			return fmt.Errorf("zlib decompress failed: %w", err)
		}
		if err := zr.Close(); err != nil {
			return err
		}
		consumed := int(inner.Size()) - inner.Len()
		offset += consumed

		var content []byte
		var finalType int
		if objType == 6 || objType == 7 {
			// delta: find base content
			var base []byte
			var baseType int
			if objType == 7 {
				// ref-delta: baseSha variable
				if b, t, ok := findObjectByShaWithType(objects, baseSha); ok {
					base = b
					baseType = t
				} else {
					// try read from existing git objects in gitDir
					if b2, err := readObjectFromGitDir(gitDir, baseSha); err == nil {
						base = b2
						baseType = 3 // assume blob if we can't determine type ### CHANGE THIS ###
					} else {
						return fmt.Errorf("base object %s not found for ref-delta", baseSha)
					}
				}
			} else {
				// ofs-delta: find base object by offset
				// baseOffset is relative to current position, we need to find the object at that position
				targetOffset := int64(offset) - baseOffset - int64(consumed)
				if b, t, ok := findObjectByOffset(objects, targetOffset); ok {
					base = b
					baseType = t
				} else {
					return fmt.Errorf("ofs-delta base at offset %d not found", targetOffset)
				}
			}
			// apply delta
			delta := decompressed.Bytes()
			reconstructed, err := applyGitDelta(base, delta)
			if err != nil {
				return fmt.Errorf("delta apply failed: %w", err)
			}
			content = reconstructed
			finalType = baseType // preserve base object type
		} else {
			// non-delta: decompressed bytes are the full object payload (header-less).
			content = decompressed.Bytes()
			finalType = objType
		}

		// map pack object type to object string for writeObject
		var typeStr string
		switch finalType {
		case 1:
			typeStr = "commit"
		case 2:
			typeStr = "tree"
		case 3:
			typeStr = "blob"
		case 4:
			typeStr = "tag"
		default:
			// fallback to blob
			typeStr = "blob"
		}

		// write using writeObject helper but into the repository's .git path: temporarily change CWD to gitDir's parent when writing files
		// use writeObject which writes into ".git/objects"; temporarily set GIT_DIR env? simpler: pass content to a helper that writes directly into gitDir
		shaHex, _, err := writeObjectIntoGitDir(typeStr, content, gitDir)
		if err != nil {
			return err
		}
		objects = append(objects, objEntry{typ: finalType, data: content, sha: shaHex, offset: objectStart})
	}
	return nil
}

// findObjectBySha searches objects slice for matching sha and returns data
func findObjectBySha(objs []objEntry, sha string) ([]byte, bool) {
	for _, o := range objs {
		if o.sha == sha {
			return o.data, true
		}
	}
	return nil, false
}

// findObjectByShaWithType searches objects slice for matching sha and returns data with type
func findObjectByShaWithType(objs []objEntry, sha string) ([]byte, int, bool) {
	for _, o := range objs {
		if o.sha == sha {
			return o.data, o.typ, true
		}
	}
	return nil, 0, false
}

// findObjectByOffset searches objects slice for object at specific pack offset
func findObjectByOffset(objs []objEntry, offset int64) ([]byte, int, bool) {
	for _, o := range objs {
		if o.offset == offset {
			return o.data, o.typ, true
		}
	}
	return nil, 0, false
}

// readObjectFromGitDir reads object content (payload after header) from gitDir/.git/objects by hex sha
func readObjectFromGitDir(gitDir string, shaHex string) ([]byte, error) {
	path := filepath.Join(gitDir, "objects", shaHex[:2], shaHex[2:])
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if _, err := io.Copy(&out, r); err != nil {
		r.Close()
		return nil, err
	}
	r.Close()
	outBytes := out.Bytes()
	if i := bytes.IndexByte(outBytes, 0); i != -1 {
		return outBytes[i+1:], nil
	}
	return nil, errors.New("invalid object")
}

// applyGitDelta implements Git's delta application algorithm.
// base: base object raw bytes, delta: delta bytes as in pack (header omitted)
func applyGitDelta(base []byte, delta []byte) ([]byte, error) {
	reader := bytes.NewReader(delta)
	// read src size (varint)
	_, err := readVarInt(reader)
	if err != nil {
		return nil, err
	}
	// read tgt size
	tgtSize, err := readVarInt(reader)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	for {
		op, err := reader.ReadByte()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if (op & 0x80) != 0 {
			// copy from base
			var off int
			var sz int
			if (op & 0x01) != 0 {
				b, _ := reader.ReadByte()
				off |= int(b)
			}
			if (op & 0x02) != 0 {
				b, _ := reader.ReadByte()
				off |= int(b) << 8
			}
			if (op & 0x04) != 0 {
				b, _ := reader.ReadByte()
				off |= int(b) << 16
			}
			if (op & 0x08) != 0 {
				b, _ := reader.ReadByte()
				off |= int(b) << 24
			}
			if (op & 0x10) != 0 {
				b, _ := reader.ReadByte()
				sz |= int(b)
			}
			if (op & 0x20) != 0 {
				b, _ := reader.ReadByte()
				sz |= int(b) << 8
			}
			if (op & 0x40) != 0 {
				b, _ := reader.ReadByte()
				sz |= int(b) << 16
			}
			if sz == 0 {
				sz = 1 << 32 // unlikely, but per spec 0 means copy full?
			}
			if off+sz > len(base) {
				return nil, errors.New("delta copy out of range")
			}
			out.Write(base[off : off+sz])
		} else {
			// insert literal of length op
			n := int(op)
			if n == 0 {
				continue
			}
			buf := make([]byte, n)
			if _, err := io.ReadFull(reader, buf); err != nil {
				return nil, err
			}
			out.Write(buf)
		}
	}
	if out.Len() != int(tgtSize) {
		// not strictly fatal; but check
		// return nil, fmt.Errorf("delta result size mismatch: got %d want %d", out.Len(), tgtSize)
	}
	return out.Bytes(), nil
}

// readVarInt reads Git-style variable-length int used in delta headers
func readVarInt(r *bytes.Reader) (int, error) {
	result := 0
	shift := 0
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		result |= int(b&0x7f) << shift
		if (b & 0x80) == 0 {
			break
		}
		shift += 7
	}
	return result, nil
}

// writeObjectIntoGitDir is like writeObject but writes into provided gitDir (path to .git)
func writeObjectIntoGitDir(objType string, content []byte, gitDir string) (string, [20]byte, error) {
	header := objType + " " + strconv.Itoa(len(content)) + "\x00"

	h := sha1.New()
	h.Write([]byte(header))
	h.Write(content)
	sum := h.Sum(nil)
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

	objectsDir := filepath.Join(gitDir, "objects", hexStr[:2])
	if err := os.MkdirAll(objectsDir, 0755); err != nil {
		return "", raw, err
	}
	objectPath := filepath.Join(objectsDir, hexStr[2:])
	if _, err := os.Stat(objectPath); os.IsNotExist(err) {
		if err := os.WriteFile(objectPath, buf.Bytes(), 0644); err != nil {
			return "", raw, err
		}
	}
	return hexStr, raw, nil
}

// checkoutCommitIntoDir reads commit object (commitSha) from .git and writes working files into targetDir
func checkoutCommitIntoDir(commitSha string, targetDir string, gitDir string) error {
	commitPayload, err := readObjectFromGitDir(gitDir, commitSha)
	if err != nil {
		return err
	}
	// find tree line
	lines := strings.Split(string(commitPayload), "\n")
	var treeSha string
	for _, l := range lines {
		if strings.HasPrefix(l, "tree ") {
			treeSha = strings.TrimSpace(strings.TrimPrefix(l, "tree "))
			break
		}
	}
	if treeSha == "" {
		return errors.New("commit has no tree")
	}
	// remove any existing files in targetDir except .git
	entries, _ := os.ReadDir(targetDir)
	for _, e := range entries {
		if e.Name() == ".git" {
			continue
		}
		os.RemoveAll(filepath.Join(targetDir, e.Name()))
	}
	// recursively checkout tree
	return checkoutTree(treeSha, targetDir, gitDir)
}

// checkoutTree writes files/directories for tree object into dir
func checkoutTree(treeSha string, dir string, gitDir string) error {
	payload, err := readObjectFromGitDir(gitDir, treeSha)
	if err != nil {
		return err
	}
	// parse entries: "<mode> <name>\x00<20_raw_sha>"
	i := 0
	for i < len(payload) {
		// read mode until space
		j := i
		for j < len(payload) && payload[j] != ' ' {
			j++
		}
		mode := string(payload[i:j])
		j++ // skip space
		// read name until NUL
		k := j
		for k < len(payload) && payload[k] != 0 {
			k++
		}
		name := string(payload[j:k])
		k++ // skip NUL
		if k+20 > len(payload) {
			return errors.New("truncated tree entry")
		}
		shaRaw := payload[k : k+20]
		shaHex := hex.EncodeToString(shaRaw)
		k += 20
		i = k

		full := filepath.Join(dir, name)
		if mode == "40000" {
			// directory
			if err := os.MkdirAll(full, 0755); err != nil {
				return err
			}
			if err := checkoutTree(shaHex, full, gitDir); err != nil {
				return err
			}
		} else {
			// blob
			content, err := readObjectFromGitDir(gitDir, shaHex)
			if err != nil {
				return err
			}
			// determine file permissions from mode
			if mode == "120000" {
				// symlink - create symlink instead of regular file ### CHANGE THIS ###
				if err := os.Symlink(string(content), full); err != nil {
					return err
				}
			} else {
				var perm os.FileMode = 0644
				if mode == "100755" {
					perm = 0755
				}
				if err := os.WriteFile(full, content, perm); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
