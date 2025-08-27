package pack

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/master-wayne7/go-git/internal/objects"
)

// PackObject represents a parsed pack object
type PackObject struct {
	Type    int
	Size    int64
	Content []byte
	Hash    string
}

// UnpackPackfile unpacks the received packfile and writes objects to .git/objects
func UnpackPackfile(packData []byte) error {
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
func writePackObjects(packObjects []*PackObject) error {
	// For simplicity, we'll assume no delta objects for now
	// In a full implementation, you'd need to resolve OFS_DELTA and REF_DELTA objects

	for _, obj := range packObjects {
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
		hexSha, _, err := objects.WriteObject(objTypeStr, obj.Content)
		if err != nil {
			return fmt.Errorf("failed to write %s object: %w", objTypeStr, err)
		}
		obj.Hash = hexSha
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
