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
  -v ~/.ssh:/root/.ssh:ro \
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
2. Mount your SSH keys so the container can authenticate with GitHub.

```bash
docker run -p 8080:8080 \
  -e JOURNAL_PASSWORD \
  -e GEMINI_API_TOKEN \
  -e GIT_USERNAME \
  -e GIT_REPO_NAME \
  -v ~/.ssh:/root/.ssh:ro \
  journal-app
```

**Note:** The container runs as root by default (Alpine), so mounting `~/.ssh` to `/root/.ssh` works. If you change the user in the Dockerfile, adjust the mount path accordingly.

## Persistence

The journal entries are stored in `journal_storage/journal.org` inside the container. If you are NOT using Git sync, you might want to mount a volume to persist this data:

```bash
docker run -p 8080:8080 \
  -e JOURNAL_PASSWORD="your_password" \
  -e GEMINI_API_TOKEN="your_gemini_token" \
  -v $(pwd)/journal_storage:/app/journal_storage \
  journal-app
```
