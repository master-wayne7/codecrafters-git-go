package clone

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/master-wayne7/go-git/internal/objects"
	"github.com/master-wayne7/go-git/internal/pack"
	"github.com/master-wayne7/go-git/internal/protocol"
)

// Clone clones a Git repository from the given URL to the specified directory
func Clone(repoUrl, dir string) error {
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
	refs, capabilities, err := protocol.DiscoverRefs(repoUrl)
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
	defaultRef := protocol.FindDefaultRef(refs)
	if defaultRef == nil {
		return fmt.Errorf("no default branch found")
	}
	fmt.Printf("Using default ref: %s -> %s\n", defaultRef.Name, defaultRef.Hash)

	// Negotiate and receive packfile
	packData, err := protocol.NegotiatePackfile(repoUrl, []*protocol.GitRef{defaultRef}, capabilities)
	if err != nil {
		return fmt.Errorf("failed to negotiate packfile: %w", err)
	}

	// Unpack the packfile
	if err := pack.UnpackPackfile(packData); err != nil {
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

// updateRefs updates local references to match the remote
func updateRefs(refs []*protocol.GitRef, defaultRef *protocol.GitRef) error {
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
	commitContent, err := objects.ReadObject(commitHash)
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
	return objects.CheckoutTree(treeHash, ".")
}
