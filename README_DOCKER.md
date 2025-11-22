# Docker Instructions

## Build the Image

```bash
docker build -t journal-app .
```

## Run the Container

To run the container, you need to provide the necessary environment variables.

### Pass-through Environment Variables

If you already have these variables exported in your local shell (e.g., in your `.bashrc` or current session), you can pass them through to the container by just specifying the flag name without a value:

```bash
docker run -p 8080:8080 \
  -e JOURNAL_PASSWORD \
  -e GEMINI_API_TOKEN \
  -e GIT_USERNAME \
  -e GIT_REPO_NAME \
  -e GITHUB_TOKEN \
  journal-app
```

### Basic Run (No Git Sync)

```bash
docker run -p 8080:8080 \
  -e JOURNAL_PASSWORD \
  -e GEMINI_API_TOKEN \
  journal-app
```

### Run with Git Sync

To enable Git sync, you need to:
1. Provide `GIT_USERNAME` and `GIT_REPO_NAME`.
2. Provide `GITHUB_TOKEN` (a Personal Access Token with repo scope).

```bash
docker run -p 8080:8080 \
  -e JOURNAL_PASSWORD \
  -e GEMINI_API_TOKEN \
  -e GIT_USERNAME \
  -e GIT_REPO_NAME \
  -e GITHUB_TOKEN \
  journal-app
```

**Note:** You no longer need to mount SSH keys. The application uses the `GITHUB_TOKEN` to authenticate via HTTPS.

## Persistence

The journal entries are stored in `journal_storage/journal.org` inside the container. If you are NOT using Git sync, you might want to mount a volume to persist this data:

```bash
docker run -p 8080:8080 \
  -e JOURNAL_PASSWORD="your_password" \
  -e GEMINI_API_TOKEN="your_gemini_token" \
  -v $(pwd)/journal_storage:/app/journal_storage \
  journal-app
```
