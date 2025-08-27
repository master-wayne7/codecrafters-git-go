# MyGit

MyGit is a simple implementation of a version control system inspired by Git. This project aims to provide a basic understanding of how Git works under the hood by implementing core functionalities such as initializing a repository, managing objects, committing changes, and cloning repositories.

## Features

- **Initialize a Repository**: Create a new Git repository with the necessary directory structure.
- **Manage Git Objects**: Read, write, and hash Git objects (blobs, trees, commits).
- **Commit Changes**: Create commit objects with associated metadata.
- **Clone Repositories**: Clone a remote Git repository and check out files.
- **Checkout Commits**: Restore files from a specific commit.

## Installation

To install MyGit, clone the repository and build the application:

```bash
git clone <repository-url>
cd mygit
go build -o mygit ./cmd/mygit
```

## Usage

After building the application, you can use it from the command line:

```bash
./mygit <command> [<args>...]
```

### Commands

- `init`: Initializes a new Git repository.
- `cat-file -p <hash>`: Displays the content of a Git object.
- `hash-object -w <file>`: Computes the hash of a file and optionally writes it as a Git object.
- `ls-tree --name-only <tree_hash>`: Lists the files in a tree object.
- `write-tree`: Writes the current directory structure as a tree object.
- `commit-tree <tree_sha> -p <parent_sha> -m <message>`: Creates a new commit object.
- `clone <repo-url> <dir>`: Clones a remote repository into the specified directory.

## Project Structure

```
codecrafters-git-go/
├── cmd/
│   └── mygit/
│       └── main.go           # Main entry point and CLI handling
├── internal/
│   ├── objects/              # Git object operations
│   │   └── objects.go        # Read/write objects, tree operations, commits
│   ├── pack/                 # Pack file handling
│   │   └── pack.go           # Pack file parsing and unpacking
│   ├── protocol/             # Git Smart HTTP protocol
│   │   └── git_http.go       # HTTP communication, refs discovery
│   └── clone/                # Clone orchestration
│       └── clone.go          # High-level clone workflow
├── go.mod                    # Go module definition
├── app/
│   └── main.go              # Original monolithic version (preserved)
└── README_MODULAR.md        # This documentation
```

## Module Overview

### 1. `internal/objects` - Git Object Operations
- **Purpose**: Handle all Git object types (blobs, trees, commits, tags)
- **Key Functions**:
  - `ReadObject()` / `WriteObject()` - Core object I/O
  - `HashObject()` - Object hashing and storage
  - `CatFile()` - Display object contents
  - `LsTree()` / `ParseTree()` - Tree operations
  - `WriteTree()` - Create tree objects from filesystem
  - `CommitTree()` - Create commit objects
  - `CheckoutTree()` - Extract tree to working directory

### 2. `internal/protocol` - Git Smart HTTP Protocol
- **Purpose**: Handle Git's Smart HTTP transfer protocol
- **Key Functions**:
  - `DiscoverRefs()` - Query repository references
  - `NegotiatePackfile()` - Request and receive pack data
  - `FindDefaultRef()` - Determine default branch
- **Types**: `GitRef` - Represents Git references

### 3. `internal/pack` - Pack File Operations
- **Purpose**: Parse and unpack Git pack files
- **Key Functions**:
  - `UnpackPackfile()` - Main pack file processing
  - `parsePackObject()` - Individual object parsing
  - Delta object handling (basic framework)
- **Types**: `PackObject` - Represents pack file objects

### 4. `internal/clone` - Clone Orchestration
- **Purpose**: Coordinate the complete clone workflow
- **Key Functions**:
  - `Clone()` - Main clone function
  - `initializeGitRepo()` - Set up repository structure
  - `updateRefs()` - Configure branches and refs
  - `checkoutWorkingTree()` - Extract files to working directory

### 5. `cmd/mygit` - Main Entry Point
- **Purpose**: CLI interface and command routing
- **Features**:
  - Command-line argument parsing
  - Error handling and user feedback
  - Integration of all internal packages

## Contributing

Contributions are welcome! Please open an issue or submit a pull request for any improvements or bug fixes.

## License

This project is licensed under the MIT License. See the LICENSE file for details.