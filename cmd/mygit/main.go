package main

import (
	"fmt"
	"os"

	"github.com/master-wayne7/go-git/internal/clone"
	"github.com/master-wayne7/go-git/internal/objects"
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
		if err := objects.CatFile(os.Args[3]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}
	case "hash-object":
		if os.Args[2] == "-w" {
			if len(os.Args) < 4 {
				fmt.Fprintf(os.Stderr, "usage: mygit hash-object -w <file>\n")
				os.Exit(1)
			}
			hash, err := objects.HashObject(os.Args[3], true)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				os.Exit(1)
			}
			fmt.Println(hash)
		} else {
			hash, err := objects.HashObject(os.Args[2], false)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				os.Exit(1)
			}
			fmt.Println(hash)
		}
	case "ls-tree":
		if os.Args[2] == "--name-only" {
			if len(os.Args) < 4 {
				fmt.Fprintf(os.Stderr, "usage: mygit ls-tree --name-only <tree_hash>\n")
				os.Exit(1)
			}
			if err := objects.LsTree(os.Args[3], true); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				os.Exit(1)
			}
		} else {
			if err := objects.LsTree(os.Args[2], false); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				os.Exit(1)
			}
		}
	case "write-tree":
		sha, err := objects.WriteTree(".")
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
		commitSha, err := objects.CommitTree(treeSha, parentSha, message)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}
		fmt.Println(commitSha)
	case "clone":
		if len(os.Args) < 4 {
			fmt.Fprintf(os.Stderr, "usage: mygit clone <repo-url> <dir>\n")
			os.Exit(1)
		}
		repoUrl := os.Args[2]
		dir := os.Args[3]
		if err := clone.Clone(repoUrl, dir); err != nil {
			fmt.Fprintf(os.Stderr, "clone failed: %s\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", command)
		os.Exit(1)
	}
}

// init command - kept as a simple function since it's straightforward
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
