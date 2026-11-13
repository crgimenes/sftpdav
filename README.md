# sftpdav

A **WebDAV** server that exposes a remote SFTP directory as a local mount point. This is useful when you have SSH access to a machine (and only port 22 is open) but need to serve a directory over **WebDAV** without running additional services on the remote server.

## Features

- **SFTP client**: Connects to a remote server via SSH and uses [SFTP](https://datatracker.ietf.org/doc/html/draft-ietf-secsh-filexfer-02).
- **WebDAV server**: Serves remote files over WebDAV on a local port.
- **No extra ports on remote**: Only SSH port `22` is required on the remote machine.
- **Survives sleep and network changes**: a keep-alive notices a dead SSH link within about 25 seconds, and the next request dials again.
- **Mountable on Linux/macOS**: Use standard WebDAV mounting tools (e.g., `mount -t davfs` on Linux, `mount_webdav` on macOS).

## How It Works

1. **SSH** connection is established using the `~/.ssh/config` file (or defaults), authenticating through `ssh-agent` or the host's `IdentityFile`.
2. **SFTP** is used to interact with the remote file system.
3. **WebDAV** is served on `127.0.0.1` only: the share has no authentication of its own.
4. Mount the local WebDAV server to access the remote files.

## Requirements

- **Go 1.27 or later**.
- A running `ssh-agent` holding your key, or an `IdentityFile` for the host in `~/.ssh/config` (an unencrypted key; a passphrase-protected one must go through the agent).
- A known_hosts file configured for the remote server (`~/.ssh/known_hosts`).

## Installation

```bash
go build
```

This will produce the binary `sftpdav`.

## Usage

1. **Run the WebDAV server**:
   ```bash
   ./sftpdav -port 8811 -host myremote
   ```
   - `-port 8811` specifies the local port to serve WebDAV (default is `8811`).
   - `-host myremote` must match a host entry from `~/.ssh/config` (or be a valid hostname).
   - `-remoteDir /path/to/dir` exposes a specific directory on the remote server (default is `"."`).

2. **Mount the WebDAV share**:

   - **Linux**:
     ```bash
     sudo mount -t davfs http://127.0.0.1:8811 /mnt/sftp
     ```
   - **macOS**:
     ```bash
     sudo mount_webdav http://127.0.0.1:8811 /mnt/sftp
     ```

3. **Unmount**:
   ```bash
   sudo umount /mnt/sftp
   ```

## Cleaning Up Extended Attributes (macOS)

On macOS, extended attributes are not supported via SFTP. The OS will create dot files (`._*`) on the remote server. To remove them:

```bash
dot_clean /path/to/your/mounted/folder
```

## Original WebDAV Server version

A simpler version of the code resides in the `webdav/` directory. It serves a local folder over WebDAV and must run on the same machine that holds the files:

```bash
go -C webdav build
./webdav/webdav -port 8811 -dir /local/folder
```

Then use an SSH tunnel to access the WebDAV server:

```bash
ssh -N -L "8811:localhost:8811" user@remote
```

## License

This project is licensed under the [MIT License](LICENSE).

## Contact

Feel free to open an issue or pull request if you have questions or improvements.

