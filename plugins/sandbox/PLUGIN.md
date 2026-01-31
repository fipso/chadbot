# Sandbox Plugin

Run isolated Docker containers with gVisor (runsc) security in rootless mode. The sandbox manages its own portable Docker installation, completely isolated from any system Docker. No root privileges required.

## Security Model

The sandbox provides different levels of isolation depending on privileges:

**Rootless mode** (default, no root required):
- User namespace isolation via rootlesskit
- Standard runc container runtime
- Process isolation from host
- No host filesystem mounts
- Separate network namespace via slirp4netns/vpnkit

**Root mode** (when running as root):
- Full gVisor `runsc` runtime with syscall interception
- Kernel-level syscall filtering and sandboxing
- Enhanced security against container escapes

Note: gVisor's `runsc` doesn't support the OCI `create` lifecycle in rootless mode, so we fall back to runc. Rootless mode still provides strong isolation through user namespaces.

## Requirements

- Linux with user namespace support (`/proc/sys/kernel/unprivileged_userns_clone` = 1)
- `newuidmap` and `newgidmap` (usually in `uidmap` or `shadow-utils` package)
- `/etc/subuid` and `/etc/subgid` configured for your user

## Skills

| Skill | Description |
|-------|-------------|
| `sandbox_status` | Check environment status and daemon state |
| `sandbox_pull` | Pull a Docker image |
| `sandbox_run` | Run a container (oneshot or detached) |
| `sandbox_exec` | Execute command in running container |
| `sandbox_images` | List available images |
| `sandbox_ps` | List containers |
| `sandbox_logs` | Get container logs |
| `sandbox_stop` | Stop a running container |
| `sandbox_rm` | Remove a container |

### sandbox_status

Check if the sandbox environment is ready.

**Parameters:** None

**Returns:** Status object with daemon state, binary paths, and Docker version.

### sandbox_pull

Pull a Docker image to the sandbox.

**Parameters:**
- `image` (required): Image to pull (e.g., `alpine:latest`, `ubuntu:22.04`)

### sandbox_run

Run a container. By default runs in foreground (oneshot) and removes on exit.

**Parameters:**
- `image` (required): Image to run
- `command` (optional): Command as JSON array (e.g., `["echo", "hello"]`)
- `rm` (optional): Remove after exit: `true`/`false` (default: `true`)
- `detach` (optional): Run in background: `true`/`false` (default: `false`)
- `ports` (optional): Port mappings as JSON array (e.g., `["8080:80"]`)
- `env` (optional): Environment variables as JSON array (e.g., `["FOO=bar"]`)
- `name` (optional): Container name

### sandbox_exec

Execute a command in a running container.

**Parameters:**
- `container` (required): Container ID or name
- `command` (required): Command as JSON array (e.g., `["ls", "-la"]`)

### sandbox_images

List images in the sandbox.

**Parameters:**
- `filter` (optional): Filter by image reference

### sandbox_ps

List containers.

**Parameters:**
- `all` (optional): Show stopped containers too: `true`/`false` (default: `false`)

### sandbox_logs

Get logs from a container.

**Parameters:**
- `container` (required): Container ID or name
- `tail` (optional): Number of lines from end (e.g., `100`)

### sandbox_stop

Stop a running container.

**Parameters:**
- `container` (required): Container ID or name

### sandbox_rm

Remove a container.

**Parameters:**
- `container` (required): Container ID or name
- `force` (optional): Force remove running container: `true`/`false` (default: `false`)

## Examples

### Throwaway Alpine shell
```
sandbox_run image="alpine" command='["sh", "-c", "echo hello && ls /"]'
```

### Run nginx with port mapping
```
sandbox_run image="nginx:alpine" detach="true" ports='["8080:80"]' name="web"
```

### Install and run a program
```
sandbox_run image="python:3.12-alpine" command='["python", "-c", "print(2+2)"]'
```

### Long-running service
```
sandbox_run image="redis:alpine" detach="true" name="cache"
sandbox_exec container="cache" command='["redis-cli", "ping"]'
sandbox_logs container="cache" tail="50"
sandbox_stop container="cache"
sandbox_rm container="cache"
```

## First Run

On first skill invocation, the sandbox will:
1. Download Docker static binaries (~100MB)
2. Download Docker rootless extras (~10MB)
3. Download gVisor runsc runtime (~50MB)
4. Start rootless Docker daemon
5. Initialize Docker data directories

This happens automatically and may take a minute on first use.

## Data Directory

All sandbox data is stored in `~/.local/share/chadbot/sandbox/`:
- `bin/` - Docker static binaries
- `runsc/` - gVisor runtime binaries
- `docker/` - Docker data (images, containers)
- `run/` - Runtime files (sockets, PIDs)
- `config/` - Configuration files
