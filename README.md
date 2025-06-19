# 🎯 Sandman: GPU SSH Gateway System

Sandman is a system that enables administrators to provision user-specific containers with GPU MIG instances and persistent volumes via API. Users can then securely connect to these containers via SSH using a dedicated port (`ssh user123@host -p PORT`).

---

## 🚀 Key Features

* Dynamic allocation and release of **GPU MIG instances**
* **Persistent volume mounting** and **isolated container creation**
* **Direct port binding** for SSH access (ports 10000–20000)
* **TTL-based session lifecycle management**

---

## 📦 Architecture Overview

```
┌────────────┐       SSH      ┌────────────┐
│   User     │ ─────────────→ │ Host:PORT  │
│ ssh user@  │                │ 10000-20000│
└────────────┘                └────────────┘
                                    │
                                    ▼
┌──────────────────────────────────────────────────────────┐
│               Orchestrator Daemon                        │
│  • Session Management  • MIG Allocation  • Port Control  │
└──────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌──────────────┐ ┌──────────────┐ ┌────────────────────────┐
│ Docker Engine│ │ NVML Library │ │ Host Volumes (/srv/...)│
└──────────────┘ └──────────────┘ └────────────────────────┘
                                    │
                                    ▼
┌──────────────────────────────────────────────────────────┐
│              Session Container                           │
│  • OpenSSH Server  • MIG Assigned  • Volume Mounted      │
│  • Bound SSH Port (10000–20000)                          │
└──────────────────────────────────────────────────────────┘
```

---

## 🛠️ Setup & Deployment

### Prerequisites

* Docker Engine 24.0+ with NVIDIA Container Runtime
* NVIDIA driver 535+ with MIG support
* Go 1.21+ (for development)

### 1. Clone the Repository

```bash
git clone https://github.com/sandman/gpu-ssh-gateway.git
cd gpu-ssh-gateway
```

### 2. Build Workspace Image

```bash
docker build -f Dockerfile.gpu-workspace -t gpu-workspace .
```

### 3. Start the System

```bash
sudo mkdir -p /srv/workspaces /var/lib/orchestrator
docker-compose up -d
```

---

## 🌐 API Overview

### CORS

* `Access-Control-Allow-Origin: *`
* Supports all methods and headers
* `credentials: include` supported
* Preflight cached for 24 hours

**Example (JavaScript):**

```javascript
fetch('http://localhost:8080/sessions', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json'
  },
  credentials: 'include',
  body: JSON.stringify({
    user_id: 'user123',
    ttl_minutes: 60
  })
})
```

---

### Health Check

```bash
GET /healthz
```

**Response:**

```json
{ "status": "healthy", "service": "gpu-ssh-gateway-orchestrator" }
```

---

## 🧑‍💻 Session Management

### Create a Session

```bash
POST /sessions
Content-Type: application/json
```

**Request:**

```json
{
  "user_id": "user123",
  "ttl_minutes": 60,
  "mig_profile": "3g.20gb",
  "image": "gpu-workspace"
}
```

**Response:**

```json
{
  "session_id": "abc-123-def-456",
  "container_id": "container_789",
  "ssh_user": "user123",
  "ssh_host": "localhost",
  "ssh_port": 10001,
  "ssh_private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n...",
  "gpu_uuid": "MIG-GPU-xxxxx",
  "created_at": "...",
  "expires_at": "..."
}
```

---

### Get Session by ID

```bash
GET /sessions/{id}
```

---

### List All Sessions

```bash
GET /sessions
```

---

### Delete a Session

```bash
DELETE /sessions/{id}
```

---

### Delete All Sessions

```bash
DELETE /sessions
```

---

## 🎮 GPU Management

### Get GPU Info

```bash
GET /gpus
```

---

### List MIG Profiles

```bash
GET /gpus/profiles
```

---

### List Available MIG Instances

```bash
GET /gpus/available
```

---

## 🧩 Environment Variables

| Variable           | Default                             | Description                |
| ------------------ | ----------------------------------- | -------------------------- |
| `--port`           | `8080`                              | API server port            |
| `--db`             | `/var/lib/orchestrator/sessions.db` | SQLite DB path             |
| `--workspace-root` | `/srv/workspaces`                   | Root directory for volumes |
| `--ssh-port-start` | `10000`                             | Start of SSH port range    |
| `--ssh-port-end`   | `20000`                             | End of SSH port range      |

---

## 🔒 Security Considerations

* Containers use `--cap-drop ALL` and `--security-opt no-new-privileges:true`
* Private Docker network (`worknet`) for container isolation
* MIG access restricted with `--gpus device=UUID`
* No root volume mounts in user containers

---

## 🧹 Session Cleanup

* Sessions expire after TTL (default: 60 min)
* Expired sessions are purged every 1 minute

  * Container stopped
  * MIG instance released
  * Port freed
  * Database record removed

---

## 🔍 Monitoring & Debugging

```bash
# View orchestrator logs
docker logs gpu-ssh-orchestrator

# View session container logs
docker logs user123-container

# Monitor GPU usage
nvidia-smi
```

---

## 🚧 Troubleshooting

* **MIG not enabled**: `sudo nvidia-smi -i 0 -mig 1` (reboot required)
* **SSH connection fails**: Ensure port is accessible and key permissions are set (chmod 600)
* **Container fails to start**: Check Docker image and MIG availability

---

## 📄 License

This project is licensed under the MIT License. See the `LICENSE` file for more information.

---

## 🙋 Support & Contributions

* Submit issues via GitHub
* Contributions welcome via Pull Requests

