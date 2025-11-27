# Usage Metrics - Quick Setup

## 1. Edit `config.yaml`

```yaml
# Enable metrics tracking
usage-statistics-enabled: true

# Set your password (plaintext or pre-hashed)
remote-management:
  secret-key: "my-secure-password-123"  # Server auto-hashes on startup
  allow-remote: false  # true = allow non-localhost access
```

**Note:** 
- Put your password in **plaintext** - the server auto-detects and hashes it on first startup
- After first run, check `config.yaml` - your plaintext will be replaced with a `$2a$...` hash
- If you provide an already-hashed value (starts with `$2a$`/`$2b$`/`$2y$`), it won't re-hash

## 2. Start Server

```bash
./server
```

Look for:
```
INFO: loaded N historical usage events from auths/usage.json
```

## 3. Open Dashboard

Visit: `http://localhost:8317/v0/management/qs/metrics/ui` 

## 4. Generate Test Data (optional)

```bash
# Make a test request to your API
curl http://localhost:8317/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "test"}]}'
```

Wait 30 seconds or click "Refresh" to see the data.

---

## 📍 Data Location

Usage data is stored at: `{auth-dir}/usage.json` (default: `./auths/usage.json`)

## 🐳 Docker

Already works! Volume mount ensures persistence:
```yaml
volumes:
  - ./auths:/root/.cli-proxy-api  # usage.json saved here
```

**That's it!** 🎉


