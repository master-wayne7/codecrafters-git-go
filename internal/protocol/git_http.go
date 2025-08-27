package protocol

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// GitRef represents a git reference
type GitRef struct {
	Name string
	Hash string
}

// DiscoverRefs discovers repository references using Git Smart HTTP protocol
func DiscoverRefs(repoUrl string) ([]*GitRef, []string, error) {
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

// NegotiatePackfile negotiates with the server to get the packfile
func NegotiatePackfile(repoUrl string, wantRefs []*GitRef, capabilities []string) ([]byte, error) {
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

// FindDefaultRef finds the default branch (main, master, or first available)
func FindDefaultRef(refs []*GitRef) *GitRef {
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

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
