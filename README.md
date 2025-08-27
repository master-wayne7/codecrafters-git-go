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

## Contributing

Contributions are welcome! Please open an issue or submit a pull request for any improvements or bug fixes.

## License

This project is licensed under the MIT License. See the LICENSE file for details.