# AI Journal

A web-based journaling application that uses Google's Gemini AI to parse unstructured thoughts into structured journal entries. It features a secure login, a Gruvbox-themed UI, and automatic synchronization with a private GitHub repository. Supports both Markdown and Org-mode output formats.

## Features

-   **AI Analysis**: Uses Gemini 2.5 Flash@latest to break down entries into:
    -   Emotional Check-in
    -   Things that made you happy
    -   Things that were stressful
    -   Focus items for next time
-   **Git Storage**: Automatically commits and pushes entries to a specified GitHub repository in Markdown or Org-mode format.
-   **Secure Access**: Simple password-based authentication with session management.
-   **Beautiful UI**: A responsive React frontend styled with the Gruvbox Dark theme.

## Prerequisites

-   **Go**: 1.18 or later
-   **Node.js**: 16 or later
-   **Git**: Configured with SSH access to GitHub.

## Configuration

The application is configured via environment variables. You must set these before running the app:

| Variable | Description | Required |
| :--- | :--- | :--- |
| `JOURNAL_PASSWORD` | The password required to log in to the web interface. | Yes |
| `GEMINI_API_TOKEN` | Your Google Gemini API key for AI processing. | Yes |
| `JOURNAL_FORMAT` | Output format for journal entries. Must be `"org"` or `"markdown"`. Defaults to `"markdown"`. | No |
| `GIT_USERNAME` | Your GitHub username (e.g., `chris`). | Yes (for sync) |
| `GIT_REPO_NAME` | The name of the private repository to store entries (e.g., `journal-entries`). | Yes (for sync) |

### Setting up the Storage Repo

1.  Create a private repository on GitHub (e.g., `journal-entries`).
2.  Ensure your local machine has SSH keys configured for your GitHub account.
3.  The application will automatically clone this repo into a `journal_storage` directory on first run.

## Running the Application

### Development Mode

To run with hot-reloading for the frontend and the Go backend:

```bash
# Set your env vars
export JOURNAL_PASSWORD="mysecretpassword"
export GEMINI_API_TOKEN="your_gemini_key"
export JOURNAL_FORMAT="markdown"  # Optional: "org" or "markdown" (default: "markdown")
export GIT_USERNAME="your_github_user"
export GIT_REPO_NAME="your_repo_name"

# Start the app
make dev
```

-   **Frontend**: http://localhost:3000
-   **Backend**: http://localhost:8080

### Production Build

To build a single binary that serves the frontend statically:

```bash
make build
make run
```

The application will be available at http://localhost:8080.

## Usage

1.  Open the app in your browser.
2.  Log in with your `JOURNAL_PASSWORD`.
3.  Type your raw thoughts into the text area and click **Save Entry**.
4.  The app will:
    -   Send the text to Gemini for analysis.
    -   Format the response into a structured entry (Markdown by default, or Org-mode if `JOURNAL_FORMAT=org` is set).
    -   Append it to `journal.md` (or `journal.org` if using Org-mode) in your Git repo.
    -   Commit and push the changes to GitHub.

## Output Format

Entries are saved in `journal.md` (default) or `journal.org` (if `JOURNAL_FORMAT=org` is set). The format can be controlled via the `JOURNAL_FORMAT` environment variable.

### Markdown Format (Default)

```markdown
## 2025-01-15 Mon

### General Emotional Checkin

Feeling productive but slightly tired.

### Things that made me happy

- Coding a new feature
- Coffee

### Things that were stressful

- Debugging a race condition

### Things I want to focus on doing for next time

- Take more breaks

### Raw Input

Today I worked on a new feature...
```

### Org-mode Format

Set `JOURNAL_FORMAT=org` to use Org-mode format:

```org
* 2025-01-15 Mon
** General Emotional Checkin
Feeling productive but slightly tired.
** Things that made me happy
- Coding a new feature
- Coffee
** Things that were stressful
- Debugging a race condition
** Things I want to focus on doing for next time
- Take more breaks
** Raw Input
Today I worked on a new feature...
```
